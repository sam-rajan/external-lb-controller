package service

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	lbClass          = "otlbc.io/lb"
	annotationScheme = "service.beta.kubernetes.io/aws-load-balancer-scheme"
	annotationLBType = "service.beta.kubernetes.io/aws-load-balancer-type"
	annotationLBName = "service.beta.kubernetes.io/aws-load-balancer-name"

	schemeInternetFacing = "internet-facing"
	schemeInternal       = "internal"

	lbTypeNLB = "nlb"
	lbTypeALB = "alb"
)

// lbNamePattern matches AWS's ELBv2 load balancer naming rules: up to 32
// alphanumeric/hyphen characters, and it must not start or end with a hyphen.
var lbNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,30}[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)

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

	if scheme, ok := svc.Annotations[annotationScheme]; ok {
		if scheme != schemeInternetFacing && scheme != schemeInternal {
			errs = append(errs, fmt.Sprintf("annotation %q must be %q or %q, got %q",
				annotationScheme, schemeInternetFacing, schemeInternal, scheme))
		}
	}

	if lbType, ok := svc.Annotations[annotationLBType]; ok {
		if lbType != lbTypeNLB && lbType != lbTypeALB {
			errs = append(errs, fmt.Sprintf("annotation %q must be %q or %q, got %q",
				annotationLBType, lbTypeNLB, lbTypeALB, lbType))
		}
	}

	if name, ok := svc.Annotations[annotationLBName]; ok {
		if !lbNamePattern.MatchString(name) {
			errs = append(errs, fmt.Sprintf("annotation %q must be 1-32 alphanumeric/hyphen characters and must not start or end with a hyphen, got %q",
				annotationLBName, name))
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid Service %s/%s: %s", svc.Namespace, svc.Name, strings.Join(errs, "; "))
	}
	return nil, nil
}

func ownedByUs(svc *corev1.Service) bool {
	return svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == lbClass
}
