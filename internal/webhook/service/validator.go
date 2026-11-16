package service

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate--v1-service,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=services,verbs=create;update,versions=v1,name=vservice.external-lb.io,admissionReviewVersions=v1

// ServiceValidator validates Service objects that use the external-lb.io/lb load balancer class.
type ServiceValidator struct{}

// SetupServiceValidatorWithManager registers the ServiceValidator webhook with the manager.
func SetupServiceValidatorWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &corev1.Service{}).
		WithValidator(&ServiceValidator{}).
		Complete()
}

func (v *ServiceValidator) ValidateCreate(_ context.Context, svc *corev1.Service) (admission.Warnings, error) {
	return validate(svc)
}

func (v *ServiceValidator) ValidateUpdate(_ context.Context, oldSvc, newSvc *corev1.Service) (admission.Warnings, error) {
	return validateUpdate(oldSvc, newSvc)
}

func (v *ServiceValidator) ValidateDelete(_ context.Context, _ *corev1.Service) (admission.Warnings, error) {
	return nil, nil
}
