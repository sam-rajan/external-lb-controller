package webhook

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	lbClass          = "otlbc.io/lb"
	annotationScheme = "service.beta.kubernetes.io/aws-load-balancer-scheme"

	schemeInternetFacing = "internet-facing"
	schemeInternal       = "internal"
)

// +kubebuilder:webhook:path=/validate--v1-service,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=services,verbs=create;update,versions=v1,name=vservice.otlbc.io,admissionReviewVersions=v1

// ServiceValidator validates Service objects that use the otlbc.io/lb load balancer class.
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

func (v *ServiceValidator) ValidateUpdate(_ context.Context, _, newSvc *corev1.Service) (admission.Warnings, error) {
	return validate(newSvc)
}

func (v *ServiceValidator) ValidateDelete(_ context.Context, _ *corev1.Service) (admission.Warnings, error) {
	return nil, nil
}

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

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid Service %s/%s: %s", svc.Namespace, svc.Name, strings.Join(errs, "; "))
	}
	return nil, nil
}

func ownedByUs(svc *corev1.Service) bool {
	return svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == lbClass
}
