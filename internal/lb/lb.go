package lb

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

// Manager delegates load balancer operations to the detected cloud Provider.
type Manager struct {
	provider Provider
}

// NewManager selects a Provider according to cfg.CloudProvider. "auto" (or
// empty) probes every registered Detector in order and uses the first that
// reports itself applicable; any other value must match a registered
// Detector's name exactly, and that Detector's own probe still runs (it is
// not used as a bypass).
func NewManager(ctx context.Context, cfg ProviderConfig) (*Manager, error) {
	if cfg.CloudProvider == "" || cfg.CloudProvider == "auto" {
		for _, nd := range registry {
			if p, ok := nd.detect(ctx, cfg); ok {
				return &Manager{provider: p}, nil
			}
		}
		return nil, fmt.Errorf("no load balancer provider detected for the current environment")
	}

	for _, nd := range registry {
		if nd.name != cfg.CloudProvider {
			continue
		}
		if p, ok := nd.detect(ctx, cfg); ok {
			return &Manager{provider: p}, nil
		}
		return nil, fmt.Errorf("cloud provider %q was requested but its environment probe failed", cfg.CloudProvider)
	}
	return nil, fmt.Errorf("cloud provider %q is not registered", cfg.CloudProvider)
}

func (m *Manager) EnsureLB(svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) (string, error) {
	return m.provider.EnsureLB(context.Background(), svc, epSlices)
}

func (m *Manager) DeleteLB(svc *corev1.Service) error {
	return m.provider.DeleteLB(context.Background(), svc)
}
