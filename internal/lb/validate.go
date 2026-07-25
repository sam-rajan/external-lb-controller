package lb

import (
	corev1 "k8s.io/api/core/v1"
)

// AnnotationValidator checks the annotations relevant to one Provider
// implementation and returns a validation error message per problem found
type AnnotationValidator func(svc *corev1.Service) []string

var validators []AnnotationValidator

func RegisterValidator(v AnnotationValidator) {
	validators = append(validators, v)
}

func ValidateAnnotations(svc *corev1.Service) []string {
	var errs []string
	for _, v := range validators {
		errs = append(errs, v(svc)...)
	}
	return errs
}
