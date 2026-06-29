package lb

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

// Manager handles load balancer lifecycle for a Service.
type Manager struct{}

// NewManager returns a new load balancer Manager.
func NewManager() *Manager {
	return &Manager{}
}

// EnsureLB creates or updates the load balancer for the given Service.
// epSlices are the EndpointSlices currently associated with the Service.
// Returns the provisioned hostname/IP to set on the Service status.
func (m *Manager) EnsureLB(svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) (hostname string, err error) {
	// TODO: implement load balancer creation/update
	return "", nil
}

// DeleteLB removes the load balancer associated with the given Service.
func (m *Manager) DeleteLB(svc *corev1.Service) error {
	// TODO: implement load balancer deletion
	return nil
}
