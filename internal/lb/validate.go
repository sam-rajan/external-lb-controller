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

// UpdateValidator checks a Service update (old vs new) for a Provider
// implementation's annotations and returns a validation error message per
// problem found, e.g. rejecting a change to a value the Provider can't
// actually update on the underlying cloud resource.
type UpdateValidator func(oldSvc, newSvc *corev1.Service) []string

var updateValidators []UpdateValidator

func RegisterUpdateValidator(v UpdateValidator) {
	updateValidators = append(updateValidators, v)
}

func ValidateAnnotationsUpdate(oldSvc, newSvc *corev1.Service) []string {
	var errs []string
	for _, v := range updateValidators {
		errs = append(errs, v(oldSvc, newSvc)...)
	}
	return errs
}
