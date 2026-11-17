package lb

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Provider manages the lifecycle of a cloud load balancer for a Service.
type Provider interface {
	// EnsureLB creates or updates the load balancer and returns the hostname or IP for the Service status.
	EnsureLB(ctx context.Context, svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) (hostname string, err error)

	DeleteLB(ctx context.Context, svc *corev1.Service) error
}

// ProviderConfig holds settings common to all Providers, taken from controller flags.
type ProviderConfig struct {
	ClusterName string

	// CloudProvider is "auto" (probe all) or a registered provider name such as "aws".
	CloudProvider string

	// K8sClient lets a Provider read cluster state, e.g. Nodes for instance targets.
	K8sClient client.Client
}
