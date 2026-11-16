package service

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/sam-rajan/external-lb-controller/internal/lb"
)

const lbClass = "external-lb.io/lb"

func validate(svc *corev1.Service) (admission.Warnings, error) {
	if !ownedByUs(svc) {
		return nil, nil
	}
	if errs := validationErrors(svc); len(errs) > 0 {
		return nil, fmt.Errorf("invalid Service %s/%s: %s", svc.Namespace, svc.Name, strings.Join(errs, "; "))
	}
	return nil, nil
}

// validateUpdate additionally enforces immutability: once a Service is owned
// by us, its loadBalancerClass can't be changed away (doing so would make the
// controller's ownedByUs check skip the Service on every future reconcile,
// orphaning its finalizer and any already-provisioned cloud resources), and
// provider-specific fields that can't be modified in place (see
// lb.ValidateAnnotationsUpdate) are likewise rejected.
func validateUpdate(oldSvc, newSvc *corev1.Service) (admission.Warnings, error) {
	var errs []string

	if ownedByUs(oldSvc) && !sameLBClass(oldSvc, newSvc) {
		errs = append(errs, fmt.Sprintf("spec.loadBalancerClass is immutable once set to %q", lbClass))
	}

	if ownedByUs(newSvc) {
		errs = append(errs, validationErrors(newSvc)...)
		if ownedByUs(oldSvc) {
			errs = append(errs, lb.ValidateAnnotationsUpdate(oldSvc, newSvc)...)
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid Service %s/%s: %s", newSvc.Namespace, newSvc.Name, strings.Join(errs, "; "))
	}
	return nil, nil
}

func validationErrors(svc *corev1.Service) []string {
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
	return errs
}

func sameLBClass(oldSvc, newSvc *corev1.Service) bool {
	var oldClass, newClass string
	if oldSvc.Spec.LoadBalancerClass != nil {
		oldClass = *oldSvc.Spec.LoadBalancerClass
	}
	if newSvc.Spec.LoadBalancerClass != nil {
		newClass = *newSvc.Spec.LoadBalancerClass
	}
	return oldClass == newClass
}

func ownedByUs(svc *corev1.Service) bool {
	return svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == lbClass
}
