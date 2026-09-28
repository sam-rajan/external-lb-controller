// Package aws registers an AWS ELB-backed load balancer provider, capable of
// provisioning either an ALB or an NLB depending on Service configuration.
package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

func (p *elbProvider) EnsureLB(ctx context.Context, svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) (string, error) {
	ownerTag := ownerTagValue(svc)

	var subnetIDs []string

	elb, err := p.findLoadBalancer(ctx, ownerTag)
	if err != nil {
		return "", fmt.Errorf("find load balancer: %w", err)
	}
	if elb == nil {
		if len(subnetIDs) == 0 {
			subnetIDs, err = p.resolveSubnetIDs(ctx, svc)
			if err != nil {
				return "", fmt.Errorf("resolve subnets: %w", err)
			}
		}
		elb, err = p.createLoadBalancer(ctx, svc, ownerTag, subnetIDs)
		if err != nil {
			return "", fmt.Errorf("create load balancer: %w", err)
		}
	}

	targets, targetPort, err := p.resolveTargets(ctx, svc, epSlices)
	if err != nil {
		return "", fmt.Errorf("resolve targets: %w", err)
	}

	for _, port := range svc.Spec.Ports {
		tg, err := p.findTargetGroup(ctx, ownerTag, port.Port)
		if err != nil {
			return "", fmt.Errorf("find target group (port %d): %w", port.Port, err)
		}
		if tg == nil {
			if len(subnetIDs) == 0 {
				subnetIDs, err = p.resolveSubnetIDs(ctx, svc)
				if err != nil {
					return "", fmt.Errorf("resolve subnets: %w", err)
				}
			}
			tg, err = p.createTargetGroup(ctx, svc, ownerTag, port, subnetIDs)
			if err != nil {
				return "", fmt.Errorf("create target group (port %d): %w", port.Port, err)
			}
		}

		if err := p.syncTargets(ctx, *tg.TargetGroupArn, targets, targetPort(port)); err != nil {
			return "", fmt.Errorf("sync targets (port %d): %w", port.Port, err)
		}

		if err := p.ensureListener(ctx, svc, *elb.LoadBalancerArn, *tg.TargetGroupArn, port); err != nil {
			return "", fmt.Errorf("ensure listener (port %d): %w", port.Port, err)
		}
	}

	return aws.ToString(elb.DNSName), nil
}

// findLoadBalancer returns the load balancer tagged with ownerTag, or nil if none exists.
func (p *elbProvider) findLoadBalancer(ctx context.Context, ownerTag string) (*types.LoadBalancer, error) {
	var allLBs []types.LoadBalancer
	paginator := elbv2.NewDescribeLoadBalancersPaginator(p.elb, &elbv2.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		allLBs = append(allLBs, page.LoadBalancers...)
	}
	if len(allLBs) == 0 {
		return nil, nil
	}

	arns := make([]string, 0, len(allLBs))
	for _, a := range allLBs {
		arns = append(arns, *a.LoadBalancerArn)
	}

	tagDescriptions, err := p.describeTags(ctx, arns)
	if err != nil {
		return nil, err
	}

	for _, td := range tagDescriptions {
		for _, t := range td.Tags {
			if aws.ToString(t.Key) == tagOwnerKey && aws.ToString(t.Value) == ownerTag {
				for _, a := range allLBs {
					if aws.ToString(a.LoadBalancerArn) == aws.ToString(td.ResourceArn) {
						return &a, nil
					}
				}
			}
		}
	}
	return nil, nil
}

func (p *elbProvider) createLoadBalancer(ctx context.Context, svc *corev1.Service, ownerTag string, subnetIDs []string) (*types.LoadBalancer, error) {
	var sgIDs []string
	if v := svc.Annotations[annotationSecurityGroups]; v != "" {
		sgIDs = splitCSV(v)
	} else {
		vpcID, err := p.vpcIDFromSubnet(ctx, subnetIDs[0])
		if err != nil {
			return nil, fmt.Errorf("resolve VPC ID: %w", err)
		}
		sgID, err := p.ensureSecurityGroup(ctx, svc, ownerTag, vpcID)
		if err != nil {
			return nil, fmt.Errorf("ensure security group: %w", err)
		}
		if err := p.reconcileSecurityGroupIngress(ctx, svc, sgID, vpcID); err != nil {
			return nil, fmt.Errorf("reconcile security group ingress: %w", err)
		}
		sgIDs = []string{sgID}
	}

	out, err := p.elb.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{
		Name:           aws.String(loadBalancerName(svc)),
		Subnets:        subnetIDs,
		SecurityGroups: sgIDs,
		Scheme:         loadBalancerScheme(svc),
		Type:           loadBalancerType(svc),
		Tags:           []types.Tag{{Key: aws.String(tagOwnerKey), Value: aws.String(ownerTag)}},
	})
	if err != nil {
		return nil, err
	}
	return &out.LoadBalancers[0], nil
}

func (p *elbProvider) DeleteLB(ctx context.Context, svc *corev1.Service) error {
	ownerTag := ownerTagValue(svc)

	elb, err := p.findLoadBalancer(ctx, ownerTag)
	if err != nil {
		return fmt.Errorf("find load balancer: %w", err)
	}
	if elb == nil {
		return nil // already gone
	}

	// Delete listeners first (required before deleting the load balancer).
	var listeners []types.Listener
	listenersPaginator := elbv2.NewDescribeListenersPaginator(p.elb, &elbv2.DescribeListenersInput{
		LoadBalancerArn: elb.LoadBalancerArn,
	})
	for listenersPaginator.HasMorePages() {
		page, err := listenersPaginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("describe listeners: %w", err)
		}
		listeners = append(listeners, page.Listeners...)
	}
	for _, l := range listeners {
		if _, err := p.elb.DeleteListener(ctx, &elbv2.DeleteListenerInput{
			ListenerArn: l.ListenerArn,
		}); err != nil {
			return fmt.Errorf("delete listener %s: %w", aws.ToString(l.ListenerArn), err)
		}
	}

	if _, err := p.elb.DeleteLoadBalancer(ctx, &elbv2.DeleteLoadBalancerInput{
		LoadBalancerArn: elb.LoadBalancerArn,
	}); err != nil {
		return fmt.Errorf("delete load balancer: %w", err)
	}

	// Wait for the load balancer to be fully deleted before releasing dependent
	// resources. AWS deletes load balancers asynchronously; SG and target groups
	// cannot be removed while it still holds references to them.
	waiter := elbv2.NewLoadBalancersDeletedWaiter(p.elb)
	if err := waiter.Wait(ctx, &elbv2.DescribeLoadBalancersInput{
		LoadBalancerArns: []string{aws.ToString(elb.LoadBalancerArn)},
	}, 5*time.Minute); err != nil {
		return fmt.Errorf("wait for load balancer deletion: %w", err)
	}

	// Delete target groups after the load balancer (they cannot be deleted while in use).
	for _, port := range svc.Spec.Ports {
		tg, err := p.findTargetGroup(ctx, ownerTag, port.Port)
		if err != nil || tg == nil {
			continue
		}
		if _, err := p.elb.DeleteTargetGroup(ctx, &elbv2.DeleteTargetGroupInput{
			TargetGroupArn: tg.TargetGroupArn,
		}); err != nil {
			return fmt.Errorf("delete target group (port %d): %w", port.Port, err)
		}
	}

	// Delete the auto-managed security group if one was created (user-provided SGs are left alone).
	if err := p.deleteMangedSecurityGroup(ctx, ownerTag, elb); err != nil {
		return fmt.Errorf("delete managed security group: %w", err)
	}

	return nil
}
