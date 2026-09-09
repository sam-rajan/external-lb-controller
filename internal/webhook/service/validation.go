package service

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/sam-rajan/out-tree-loadbalancer-controller/internal/lb"
)

const lbClass = "otlbc.io/lb"

func validate(svc *corev1.Service) (admission.Warnings, error) {
	if !ownedByUs(svc) {
		return nil, nil
	}

	var errs []string

	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		errs = append(errs, fmt.Sprintf("spec.type must be LoadBalancer when loadBalancerClass is %q, got %q", lbClass, svc.Spec.Type))
	}

	if len(svc.Spec.Ports) == 0 {
		errs = append(errs, "spec.ports must not be empty")
	}

	for i, p := range svc.Spec.Ports {
		if p.Protocol != corev1.ProtocolTCP {
			errs = append(errs, fmt.Sprintf("spec.ports[%d]: only TCP protocol is supported, got %q", i, p.Protocol))
		}
	}

	errs = append(errs, lb.ValidateAnnotations(svc)...)

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid Service %s/%s: %s", svc.Namespace, svc.Name, strings.Join(errs, "; "))
	}
	return nil, nil
}

func ownedByUs(svc *corev1.Service) bool {
	return svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == lbClass
}
