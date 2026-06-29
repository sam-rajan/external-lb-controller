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

// NewManager iterates all registered Detectors and returns a Manager backed by
// the first Provider that reports itself as applicable for the current environment.
func NewManager(ctx context.Context) (*Manager, error) {
	for _, detect := range registry {
		if p, ok := detect(ctx); ok {
			return &Manager{provider: p}, nil
		}
	}
	return nil, fmt.Errorf("no load balancer provider detected for the current environment")
}

func (m *Manager) EnsureLB(svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) (string, error) {
	return m.provider.EnsureLB(context.Background(), svc, epSlices)
}

func (m *Manager) DeleteLB(svc *corev1.Service) error {
	return m.provider.DeleteLB(context.Background(), svc)
}
