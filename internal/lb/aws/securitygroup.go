package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
)

const sgNameSuffix = "-sg"

// securityGroupName derives the auto-managed security group's name from the
// resolved load balancer name, swapping its "-lb" suffix for "-sg" so the two
// stay paired regardless of whether the LB name came from the annotation or
// the derived default.
func securityGroupName(svc *corev1.Service) string {
	base := strings.TrimSuffix(loadBalancerName(svc), lbNameSuffix)
	return withSuffix(base, sgNameSuffix)
}

// ensureSecurityGroup finds the auto-managed SG for this service, or creates one.
func (p *elbProvider) ensureSecurityGroup(ctx context.Context, svc *corev1.Service, ownerTag, vpcID string) (string, error) {
	out, err := p.ec2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("tag:" + tagOwnerKey), Values: []string{ownerTag}},
		},
	})
	if err != nil {
		return "", err
	}
	if len(out.SecurityGroups) > 0 {
		return aws.ToString(out.SecurityGroups[0].GroupId), nil
	}

	created, err := p.ec2.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(securityGroupName(svc)),
		Description: aws.String("Managed by otlbc for " + ownerTag),
		VpcId:       aws.String(vpcID),
		TagSpecifications: []ec2types.TagSpecification{{
			ResourceType: ec2types.ResourceTypeSecurityGroup,
			Tags: []ec2types.Tag{
				{Key: aws.String(tagOwnerKey), Value: aws.String(ownerTag)},
			},
		}},
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(created.GroupId), nil
}

// reconcileSecurityGroupIngress ensures the auto-managed SG allows inbound
// TCP traffic on each of the Service's listener ports from the resolved
// source ranges, adding/removing rules to match exactly (this SG is fully
// owned by the controller, so any rule not in the desired set is stale).
func (p *elbProvider) reconcileSecurityGroupIngress(ctx context.Context, svc *corev1.Service, sgID, vpcID string) error {
	ranges, err := p.resolveSourceRanges(ctx, svc, vpcID)
	if err != nil {
		return fmt.Errorf("resolve source ranges: %w", err)
	}

	desired := make(map[int32]map[string]struct{}, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		cidrs := make(map[string]struct{}, len(ranges))
		for _, r := range ranges {
			cidrs[r] = struct{}{}
		}
		desired[port.Port] = cidrs
	}

	out, err := p.ec2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{sgID},
	})
	if err != nil {
		return fmt.Errorf("describe security group: %w", err)
	}
	if len(out.SecurityGroups) == 0 {
		return fmt.Errorf("security group %s not found", sgID)
	}

	current := make(map[int32]map[string]struct{})
	for _, perm := range out.SecurityGroups[0].IpPermissions {
		if aws.ToString(perm.IpProtocol) != "tcp" || perm.FromPort == nil || perm.ToPort == nil || *perm.FromPort != *perm.ToPort {
			continue // leave rules we don't manage (non-TCP or port ranges) untouched
		}
		port := *perm.FromPort
		if current[port] == nil {
			current[port] = make(map[string]struct{})
		}
		for _, r := range perm.IpRanges {
			current[port][aws.ToString(r.CidrIp)] = struct{}{}
		}
	}

	var toAdd, toRemove []ec2types.IpPermission
	for port, cidrs := range desired {
		var add []ec2types.IpRange
		for cidr := range cidrs {
			if _, ok := current[port][cidr]; !ok {
				add = append(add, ec2types.IpRange{CidrIp: aws.String(cidr)})
			}
		}
		if len(add) > 0 {
			toAdd = append(toAdd, tcpIpPermission(port, add))
		}
	}
	for port, cidrs := range current {
		var remove []ec2types.IpRange
		for cidr := range cidrs {
			if _, ok := desired[port][cidr]; !ok {
				remove = append(remove, ec2types.IpRange{CidrIp: aws.String(cidr)})
			}
		}
		if len(remove) > 0 {
			toRemove = append(toRemove, tcpIpPermission(port, remove))
		}
	}

	if len(toRemove) > 0 {
		if _, err := p.ec2.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: toRemove,
		}); err != nil {
			return fmt.Errorf("revoke stale ingress rules: %w", err)
		}
	}
	if len(toAdd) > 0 {
		if _, err := p.ec2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: toAdd,
		}); err != nil {
			return fmt.Errorf("authorize ingress rules: %w", err)
		}
	}
	return nil
}

func tcpIpPermission(port int32, ranges []ec2types.IpRange) ec2types.IpPermission {
	return ec2types.IpPermission{
		IpProtocol: aws.String("tcp"),
		FromPort:   aws.Int32(port),
		ToPort:     aws.Int32(port),
		IpRanges:   ranges,
	}
}

func (p *elbProvider) deleteMangedSecurityGroup(ctx context.Context, ownerTag string, elb *types.LoadBalancer) error {
	vpcID := ""
	for _, az := range elb.AvailabilityZones {
		if az.SubnetId != nil {
			id, err := p.vpcIDFromSubnet(ctx, *az.SubnetId)
			if err == nil {
				vpcID = id
				break
			}
		}
	}
	if vpcID == "" {
		return nil
	}

	out, err := p.ec2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("tag:" + tagOwnerKey), Values: []string{ownerTag}},
		},
	})
	if err != nil || len(out.SecurityGroups) == 0 {
		return err
	}

	for _, sg := range out.SecurityGroups {
		if err := p.deleteSecurityGroupWithRetry(ctx, aws.ToString(sg.GroupId)); err != nil {
			return err
		}
	}
	return nil
}

func (p *elbProvider) deleteSecurityGroupWithRetry(ctx context.Context, sgID string) error {
	const maxAttempts = 10

	var apiErr interface{ ErrorCode() string }
	delay := 5 * time.Second
	for range maxAttempts {
		_, err := p.ec2.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(sgID),
		})
		if err == nil {
			return nil
		}
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "DependencyViolation" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				delay *= 2
				continue
			}
		}
		return err
	}
	return fmt.Errorf("security group %s still has dependencies after %d attempts", sgID, maxAttempts)
}
