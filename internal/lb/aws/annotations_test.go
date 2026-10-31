package aws

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func svcWith(annotations map[string]string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "web", Annotations: annotations}}
}

func TestLoadBalancerType(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantType    types.LoadBalancerTypeEnum
		wantProto   types.ProtocolEnum
	}{
		{"default is NLB/TCP", nil, types.LoadBalancerTypeEnumNetwork, types.ProtocolEnumTcp},
		{"explicit nlb", map[string]string{annotationLBType: "nlb"}, types.LoadBalancerTypeEnumNetwork, types.ProtocolEnumTcp},
		{"alb", map[string]string{annotationLBType: "alb"}, types.LoadBalancerTypeEnumApplication, types.ProtocolEnumHttp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := svcWith(tt.annotations)
			if got := loadBalancerType(svc); got != tt.wantType {
				t.Errorf("loadBalancerType = %v, want %v", got, tt.wantType)
			}
			if got := backendProtocol(svc); got != tt.wantProto {
				t.Errorf("backendProtocol = %v, want %v", got, tt.wantProto)
			}
		})
	}
}

func TestLoadBalancerScheme(t *testing.T) {
	if got := loadBalancerScheme(svcWith(nil)); got != types.LoadBalancerSchemeEnumInternetFacing {
		t.Errorf("default scheme = %v, want internet-facing", got)
	}
	if got := loadBalancerScheme(svcWith(map[string]string{annotationScheme: "internal"})); got != types.LoadBalancerSchemeEnumInternal {
		t.Errorf("scheme = %v, want internal", got)
	}
}

func TestTargetType(t *testing.T) {
	if got := targetType(svcWith(nil)); got != types.TargetTypeEnumInstance {
		t.Errorf("default target type = %v, want instance", got)
	}
	if got := targetType(svcWith(map[string]string{annotationTargetType: "ip"})); got != types.TargetTypeEnumIp {
		t.Errorf("target type = %v, want ip", got)
	}
}
