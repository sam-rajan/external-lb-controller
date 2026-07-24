package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
)

// resolveSubnetIDs returns annotationSubnets if set, otherwise discovers
// subnets by tag for the Service's scheme.
func (p *elbProvider) resolveSubnetIDs(ctx context.Context, svc *corev1.Service) ([]string, error) {
	if subnets := svc.Annotations[annotationSubnets]; subnets != "" {
		return splitCSV(subnets), nil
	}
	return p.discoverSubnets(ctx, loadBalancerScheme(svc))
}

const (
	// Standard EKS subnet tagging convention for auto-discovery.
	subnetTagRoleInternetFacing = "kubernetes.io/role/elb"
	subnetTagRoleInternal       = "kubernetes.io/role/internal-elb"
)

// discoverSubnets finds subnets tagged for this cluster
// (kubernetes.io/cluster/<clusterName>=shared|owned) and for the requested
// scheme (kubernetes.io/role/elb for internet-facing, .../internal-elb for
// internal), used when a Service doesn't set annotationSubnets.
func (p *elbProvider) discoverSubnets(ctx context.Context, scheme types.LoadBalancerSchemeEnum) ([]string, error) {
	if p.clusterName == "" {
		return nil, fmt.Errorf("service is missing required annotation %q and no --cluster-name is configured for automatic subnet discovery", annotationSubnets)
	}

	roleTag := subnetTagRoleInternetFacing
	if scheme == types.LoadBalancerSchemeEnumInternal {
		roleTag = subnetTagRoleInternal
	}
	clusterTag := "kubernetes.io/cluster/" + p.clusterName

	out, err := p.ec2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:" + clusterTag), Values: []string{"shared", "owned"}},
			// "tag-key" matches subnets that have the role tag present,
			// regardless of its value ("1" or "" are both used in practice).
			{Name: aws.String("tag-key"), Values: []string{roleTag}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe subnets: %w", err)
	}
	if len(out.Subnets) == 0 {
		return nil, fmt.Errorf("no subnets found tagged %q=shared|owned and %q for cluster %q", clusterTag, roleTag, p.clusterName)
	}

	ids := make([]string, 0, len(out.Subnets))
	for _, s := range out.Subnets {
		ids = append(ids, aws.ToString(s.SubnetId))
	}
	return ids, nil
}

func (p *elbProvider) vpcIDFromSubnet(ctx context.Context, subnetID string) (string, error) {
	out, err := p.ec2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		SubnetIds: []string{subnetID},
	})
	if err != nil {
		return "", err
	}
	if len(out.Subnets) == 0 {
		return "", fmt.Errorf("subnet %s not found", subnetID)
	}
	return aws.ToString(out.Subnets[0].VpcId), nil
}

// resolveSourceRanges determines which CIDRs are allowed to reach the load
// balancer's listeners: svc.Spec.LoadBalancerSourceRanges always wins if set;
// otherwise internet-facing LBs default to 0.0.0.0/0 and internal LBs default
// to the VPC's own CIDR block(s).
func (p *elbProvider) resolveSourceRanges(ctx context.Context, svc *corev1.Service, vpcID string) ([]string, error) {
	if len(svc.Spec.LoadBalancerSourceRanges) > 0 {
		return svc.Spec.LoadBalancerSourceRanges, nil
	}

	if svc.Annotations[annotationScheme] != "internal" {
		return []string{"0.0.0.0/0"}, nil
	}

	out, err := p.ec2.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{vpcID}})
	if err != nil {
		return nil, fmt.Errorf("describe VPC %s: %w", vpcID, err)
	}
	if len(out.Vpcs) == 0 {
		return nil, fmt.Errorf("VPC %s not found", vpcID)
	}

	var cidrs []string
	for _, assoc := range out.Vpcs[0].CidrBlockAssociationSet {
		if assoc.CidrBlockState == nil || assoc.CidrBlockState.State != ec2types.VpcCidrBlockStateCodeAssociated {
			continue
		}
		cidrs = append(cidrs, aws.ToString(assoc.CidrBlock))
	}
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("VPC %s has no associated CIDR blocks", vpcID)
	}
	return cidrs, nil
}
