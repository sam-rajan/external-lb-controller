package aws

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

func (p *elbProvider) resolveTargets(ctx context.Context, svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) ([]string, func(corev1.ServicePort) int, error) {
	if svc.Annotations[annotationTargetType] == targetTypeIP {
		return endpointTargets(epSlices), func(port corev1.ServicePort) int { return int(port.TargetPort.IntVal) }, nil
	}

	ids, err := p.instanceTargets(ctx, epSlices)
	if err != nil {
		return nil, nil, err
	}
	return ids, func(port corev1.ServicePort) int { return int(port.NodePort) }, nil
}

func (p *elbProvider) instanceTargets(ctx context.Context, epSlices []discoveryv1.EndpointSlice) ([]string, error) {
	nodeNames := readyNodeNames(epSlices)
	ids := make([]string, 0, len(nodeNames))
	for _, name := range nodeNames {
		id, err := p.instanceIDForNode(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("resolve instance ID for node %s: %w", name, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// readyNodeNames returns the distinct set of node names hosting at least one
// ready endpoint, across all given EndpointSlices.
func readyNodeNames(epSlices []discoveryv1.EndpointSlice) []string {
	seen := make(map[string]struct{})
	for _, eps := range epSlices {
		for _, ep := range eps.Endpoints {
			if ep.Conditions.Ready == nil || !*ep.Conditions.Ready || ep.NodeName == nil {
				continue
			}
			seen[*ep.NodeName] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}

// instanceIDForNode resolves a Node's EC2 instance ID from its providerID,
// which AWS's cloud provider sets in the form "aws:///<az>/<instance-id>".
func (p *elbProvider) instanceIDForNode(ctx context.Context, nodeName string) (string, error) {
	node := &corev1.Node{}
	if err := p.k8sClient.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return "", fmt.Errorf("get node %s: %w", nodeName, err)
	}

	idx := strings.LastIndex(node.Spec.ProviderID, "/")
	if idx == -1 || idx == len(node.Spec.ProviderID)-1 {
		return "", fmt.Errorf("node %s has no usable providerID (got %q)", nodeName, node.Spec.ProviderID)
	}
	return node.Spec.ProviderID[idx+1:], nil
}
