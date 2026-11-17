package lb

import (
	corev1 "k8s.io/api/core/v1"
)

// AnnotationValidator validates one Provider's annotations, returning a message per problem.
type AnnotationValidator func(svc *corev1.Service) []string

var validators []AnnotationValidator

func RegisterValidator(v AnnotationValidator) {
	validators = append(validators, v)
}

func ValidateAnnotations(svc *corev1.Service) []string {
	errs := make([]string, 0, len(validators))
	for _, v := range validators {
		errs = append(errs, v(svc)...)
	}
	return errs
}

// UpdateValidator validates a Service update (old vs new) for one Provider's annotations.
type UpdateValidator func(oldSvc, newSvc *corev1.Service) []string

var updateValidators []UpdateValidator

func RegisterUpdateValidator(v UpdateValidator) {
	updateValidators = append(updateValidators, v)
}

func ValidateAnnotationsUpdate(oldSvc, newSvc *corev1.Service) []string {
	errs := make([]string, 0, len(updateValidators))
	for _, v := range updateValidators {
		errs = append(errs, v(oldSvc, newSvc)...)
	}
	return errs
}
