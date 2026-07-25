// Package aws registers an AWS ELB-backed load balancer provider, capable of
// provisioning either an ALB or an NLB depending on Service configuration.
package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"

	"samrajan.me/otlbc/internal/lb"
)

const (
	// Standard AWS load balancer annotations (compatible with aws-load-balancer-controller).
	annotationSubnets        = "service.beta.kubernetes.io/aws-load-balancer-subnets"
	annotationSecurityGroups = "service.beta.kubernetes.io/aws-load-balancer-security-groups"
	annotationScheme         = "service.beta.kubernetes.io/aws-load-balancer-scheme"    // "internet-facing" (default) or "internal"
	annotationLBType         = "service.beta.kubernetes.io/aws-load-balancer-type"      // "nlb" (default) or "alb"
	annotationLBName         = "service.beta.kubernetes.io/aws-load-balancer-name"      // custom name, defaults to "<namespace>-<name>"
	annotationSSLCert        = "service.beta.kubernetes.io/aws-load-balancer-ssl-cert"  // comma-separated ACM cert ARNs; first is the default cert
	annotationSSLPorts       = "service.beta.kubernetes.io/aws-load-balancer-ssl-ports" // comma-separated ports, or "*"/absent for all ports (only meaningful if ssl-cert is set)

	// Tag applied to every AWS resource managed by this controller.
	tagOwnerKey = "otlbc.io/service"
)

func init() {
	lb.Register(detect)
}

// detect tries to load AWS credentials. If successful the controller is
// running in an AWS environment and the ELB provider is used.
func detect(ctx context.Context, providerCfg lb.ProviderConfig) (lb.Provider, bool) {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, false
	}
	return &elbProvider{
		elb:         elbv2.NewFromConfig(awsCfg),
		ec2:         ec2.NewFromConfig(awsCfg),
		clusterName: providerCfg.ClusterName,
	}, true
}

type elbProvider struct {
	elb         *elbv2.Client
	ec2         *ec2.Client
	clusterName string
}

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

	targets := endpointTargets(epSlices)

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

		if err := p.syncTargets(ctx, *tg.TargetGroupArn, targets, int(port.TargetPort.IntVal)); err != nil {
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
	out, err := p.elb.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{})
	if err != nil {
		return nil, err
	}
	if len(out.LoadBalancers) == 0 {
		return nil, nil
	}

	arns := make([]string, 0, len(out.LoadBalancers))
	for _, a := range out.LoadBalancers {
		arns = append(arns, *a.LoadBalancerArn)
	}

	tagsOut, err := p.elb.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: arns})
	if err != nil {
		return nil, err
	}

	for _, td := range tagsOut.TagDescriptions {
		for _, t := range td.Tags {
			if aws.ToString(t.Key) == tagOwnerKey && aws.ToString(t.Value) == ownerTag {
				for _, a := range out.LoadBalancers {
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
	listenersOut, err := p.elb.DescribeListeners(ctx, &elbv2.DescribeListenersInput{
		LoadBalancerArn: elb.LoadBalancerArn,
	})
	if err != nil {
		return fmt.Errorf("describe listeners: %w", err)
	}
	for _, l := range listenersOut.Listeners {
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

// loadBalancerType returns the ELBv2 load balancer type requested via
// annotationLBType, defaulting to Network Load Balancer (matching the
// TCP-only port validation enforced by the admission webhook). The
// type-specific values themselves live in nlb.go / alb.go.
func loadBalancerType(svc *corev1.Service) types.LoadBalancerTypeEnum {
	if svc.Annotations[annotationLBType] == lbTypeALB {
		return albType
	}
	return nlbType
}

// backendProtocol returns the plaintext target group / listener protocol
// matching the load balancer type: TCP for NLB, HTTP for ALB.
func backendProtocol(svc *corev1.Service) types.ProtocolEnum {
	if svc.Annotations[annotationLBType] == lbTypeALB {
		return albBackendProtocol
	}
	return nlbBackendProtocol
}

// loadBalancerScheme resolves the "internet-facing" (default) vs "internal"
// scheme from annotationScheme.
func loadBalancerScheme(svc *corev1.Service) types.LoadBalancerSchemeEnum {
	if svc.Annotations[annotationScheme] == "internal" {
		return types.LoadBalancerSchemeEnumInternal
	}
	return types.LoadBalancerSchemeEnumInternetFacing
}
