package aws

import (
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"

	"github.com/sam-rajan/out-tree-loadbalancer-controller/internal/lb"
)

func init() {
	lb.RegisterValidator(validateAnnotations)
	lb.RegisterUpdateValidator(validateAnnotationImmutability)
}

// immutableAnnotations can't be changed once set: AWS doesn't support
// modifying scheme, load balancer name, or target type on an existing
// load balancer/target group in place (see docs/design.md's create-only
// limitation), so an edit would otherwise silently do nothing.
var immutableAnnotations = []string{annotationScheme, annotationLBName, annotationTargetType}

func validateAnnotationImmutability(oldSvc, newSvc *corev1.Service) []string {
	var errs []string
	for _, key := range immutableAnnotations {
		oldVal, newVal := oldSvc.Annotations[key], newSvc.Annotations[key]
		if oldVal != newVal {
			errs = append(errs, fmt.Sprintf("annotation %q is immutable once set (the underlying AWS resource can't be modified in place): cannot change from %q to %q",
				key, oldVal, newVal))
		}
	}
	return errs
}

// lbNamePattern matches AWS's ELBv2 load balancer naming rules: up to 32
// alphanumeric/hyphen characters, and it must not start or end with a hyphen.
var lbNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,30}[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)

// validateAnnotations checks the aws-load-balancer-* annotations this
// provider understands, registered with lb.RegisterValidator so the
// admission webhook can validate them without depending on this package
// directly (mirrors how internal/controller only depends on lb.Manager).
func validateAnnotations(svc *corev1.Service) []string {
	var errs []string

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

	if targetType, ok := svc.Annotations[annotationTargetType]; ok {
		if targetType != targetTypeInstance && targetType != targetTypeIP {
			errs = append(errs, fmt.Sprintf("annotation %q must be %q or %q, got %q",
				annotationTargetType, targetTypeInstance, targetTypeIP, targetType))
		}
	}

	return errs
}
