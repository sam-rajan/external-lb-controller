package lb

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

// Provider manages the lifecycle of a cloud load balancer for a Kubernetes Service.
type Provider interface {
	// EnsureLB creates or updates the load balancer for svc using the given endpoint slices.
	// Returns the provisioned hostname or IP to publish on the Service status.
	EnsureLB(ctx context.Context, svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) (hostname string, err error)

	// DeleteLB removes the load balancer associated with svc.
	DeleteLB(ctx context.Context, svc *corev1.Service) error
}

// ProviderConfig carries configuration common to all Providers, sourced from
// controller flags rather than per-Service annotations.
type ProviderConfig struct {
	// ClusterName identifies this Kubernetes cluster for tag-based subnet
	// auto-discovery (kubernetes.io/cluster/<ClusterName>=shared|owned).
	ClusterName string
}

// Detector probes the runtime environment and returns a Provider if this
// implementation is applicable, or (nil, false) if not.
type Detector func(ctx context.Context, cfg ProviderConfig) (Provider, bool)

var registry []Detector

// Register adds a Detector to the auto-detection registry.
// Cloud-specific packages call this from their init() functions.
func Register(d Detector) {
	registry = append(registry, d)
}
