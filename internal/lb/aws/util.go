package aws

import (
	"context"
	"strings"

	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

// describeTagsMaxARNs is the maximum number of resource ARNs the ELBv2
// DescribeTags API accepts in a single call.
const describeTagsMaxARNs = 20

// describeTags fetches tag descriptions for arbitrarily many resource ARNs,
// batching requests to stay within the DescribeTags API's per-call limit.
func (p *elbProvider) describeTags(ctx context.Context, arns []string) ([]types.TagDescription, error) {
	var all []types.TagDescription
	for i := 0; i < len(arns); i += describeTagsMaxARNs {
		end := min(i+describeTagsMaxARNs, len(arns))
		out, err := p.elb.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: arns[i:end]})
		if err != nil {
			return nil, err
		}
		all = append(all, out.TagDescriptions...)
	}
	return all, nil
}

func ownerTagValue(svc *corev1.Service) string {
	return svc.Namespace + "/" + svc.Name
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// endpointTargets collects all ready endpoint IPs from the given EndpointSlices.
func endpointTargets(epSlices []discoveryv1.EndpointSlice) []string {
	var ips []string
	for _, eps := range epSlices {
		for _, ep := range eps.Endpoints {
			if ep.Conditions.Ready == nil || !*ep.Conditions.Ready {
				continue
			}
			ips = append(ips, ep.Addresses...)
		}
	}
	return ips
}
