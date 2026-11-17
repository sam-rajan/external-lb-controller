package lb

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

type Manager struct {
	provider Provider
}

// NewManager picks a Provider: "auto" probes registered Detectors in order; a named provider
// must still pass its own probe.
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

func (m *Manager) EnsureLB(ctx context.Context, svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) (string, error) {
	return m.provider.EnsureLB(ctx, svc, epSlices)
}

func (m *Manager) DeleteLB(ctx context.Context, svc *corev1.Service) error {
	return m.provider.DeleteLB(ctx, svc)
}
