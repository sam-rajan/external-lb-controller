package service

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Registers the AWS annotation validators with internal/lb.
	_ "github.com/sam-rajan/out-tree-loadbalancer-controller/internal/lb/aws"
)

const (
	annScheme     = "service.beta.kubernetes.io/aws-load-balancer-scheme"
	annLBType     = "service.beta.kubernetes.io/aws-load-balancer-type"
	annTargetType = "service.beta.kubernetes.io/aws-load-balancer-target-type"
)

func newSvc(mutate func(*corev1.Service)) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "web"},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: new(lbClass),
			Ports:             []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
		},
	}
	if mutate != nil {
		mutate(svc)
	}
	return svc
}

func TestValidateCreate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*corev1.Service)
		wantErr string // empty means the Service must be accepted
	}{
		{"valid", nil, ""},
		{"valid with annotations", func(s *corev1.Service) {
			s.Annotations = map[string]string{annScheme: "internal", annLBType: "nlb", annTargetType: "ip"}
		}, ""},
		{"other class is ignored even if invalid", func(s *corev1.Service) {
			s.Spec.LoadBalancerClass = new("example.com/other")
			s.Spec.Ports = nil
			s.Annotations = map[string]string{annScheme: "bogus"}
		}, ""},
		{"no class is ignored", func(s *corev1.Service) { s.Spec.LoadBalancerClass = nil; s.Spec.Ports = nil }, ""},
		{"wrong type", func(s *corev1.Service) { s.Spec.Type = corev1.ServiceTypeClusterIP }, "spec.type must be LoadBalancer"},
		{"no ports", func(s *corev1.Service) { s.Spec.Ports = nil }, "spec.ports must not be empty"},
		{"udp port", func(s *corev1.Service) { s.Spec.Ports[0].Protocol = corev1.ProtocolUDP }, "only TCP"},
		{"sctp port", func(s *corev1.Service) {
			s.Spec.Ports = append(s.Spec.Ports, corev1.ServicePort{Port: 81, Protocol: corev1.ProtocolSCTP})
		}, "spec.ports[1]"},
		{"bad scheme", func(s *corev1.Service) { s.Annotations = map[string]string{annScheme: "public"} }, annScheme},
	}
	v := &ServiceValidator{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateCreate(context.Background(), newSvc(tt.mutate))
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestValidateUpdate(t *testing.T) {
	tests := []struct {
		name     string
		old, new func(*corev1.Service)
		wantErr  string
	}{
		{"no change", nil, nil, ""},
		{"class changed away", nil, func(s *corev1.Service) { s.Spec.LoadBalancerClass = new("example.com/other") },
			"spec.loadBalancerClass is immutable"},
		{"class removed", nil, func(s *corev1.Service) { s.Spec.LoadBalancerClass = nil },
			"spec.loadBalancerClass is immutable"},
		{"scheme changed",
			func(s *corev1.Service) { s.Annotations = map[string]string{annScheme: "internal"} },
			func(s *corev1.Service) { s.Annotations = map[string]string{annScheme: "internet-facing"} },
			"immutable once set"},
		{"target type added", nil,
			func(s *corev1.Service) { s.Annotations = map[string]string{annTargetType: "ip"} },
			"immutable once set"},
		{"update makes service invalid", nil,
			func(s *corev1.Service) { s.Spec.Ports[0].Protocol = corev1.ProtocolUDP }, "only TCP"},
		{"foreign service may change class to ours",
			func(s *corev1.Service) { s.Spec.LoadBalancerClass = nil }, nil, ""},
		{"foreign service stays unvalidated",
			func(s *corev1.Service) { s.Spec.LoadBalancerClass = new("x/y") },
			func(s *corev1.Service) { s.Spec.LoadBalancerClass = new("x/y"); s.Spec.Ports = nil }, ""},
	}
	v := &ServiceValidator{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateUpdate(context.Background(), newSvc(tt.old), newSvc(tt.new))
			checkErr(t, err, tt.wantErr)
		})
	}
}

func TestValidateDeleteAlwaysAllowed(t *testing.T) {
	if _, err := (&ServiceValidator{}).ValidateDelete(context.Background(), newSvc(nil)); err != nil {
		t.Errorf("delete rejected: %v", err)
	}
}

func checkErr(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Errorf("unexpected error: %v", err)
	case want != "" && err == nil:
		t.Errorf("want error containing %q, got nil", want)
	case want != "" && !strings.Contains(err.Error(), want):
		t.Errorf("error %q does not contain %q", err, want)
	}
}
