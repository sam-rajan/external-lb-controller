// Package aws registers an AWS ALB-backed load balancer provider.
// Import this package with a blank import in main to activate it:
//
//	import _ "samrajan.me/otlbc/internal/lb/aws"
package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"

	"samrajan.me/otlbc/internal/lb"
)

const (
	// Annotations on the Service that must be set by the user.
	annotationSubnets        = "samrajan.me/subnets"         // comma-separated subnet IDs
	annotationSecurityGroups = "samrajan.me/security-groups" // comma-separated SG IDs

	// Tag applied to every AWS resource managed by this controller.
	tagOwnerKey = "samrajan.me/service"
)

func init() {
	lb.Register(detect)
}

// detect tries to load AWS credentials. If successful the controller is
// running in (or configured for) an AWS environment and the ALB provider is used.
func detect(ctx context.Context) (lb.Provider, bool) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, false
	}
	return &albProvider{
		client: elbv2.NewFromConfig(cfg),
	}, true
}

type albProvider struct {
	client *elbv2.Client
}

// ──────────────────────────────────────────────────────────────────────────────
// EnsureLB
// ──────────────────────────────────────────────────────────────────────────────

func (p *albProvider) EnsureLB(ctx context.Context, svc *corev1.Service, epSlices []discoveryv1.EndpointSlice) (string, error) {
	ownerTag := ownerTagValue(svc)

	alb, err := p.findALB(ctx, ownerTag)
	if err != nil {
		return "", fmt.Errorf("find ALB: %w", err)
	}
	if alb == nil {
		alb, err = p.createALB(ctx, svc, ownerTag)
		if err != nil {
			return "", fmt.Errorf("create ALB: %w", err)
		}
	}

	targets := endpointTargets(epSlices)

	for _, port := range svc.Spec.Ports {
		tg, err := p.findTargetGroup(ctx, ownerTag, port.Port)
		if err != nil {
			return "", fmt.Errorf("find target group (port %d): %w", port.Port, err)
		}
		if tg == nil {
			tg, err = p.createTargetGroup(ctx, svc, ownerTag, port)
			if err != nil {
				return "", fmt.Errorf("create target group (port %d): %w", port.Port, err)
			}
		}

		if err := p.syncTargets(ctx, *tg.TargetGroupArn, targets, int(port.TargetPort.IntVal)); err != nil {
			return "", fmt.Errorf("sync targets (port %d): %w", port.Port, err)
		}

		if err := p.ensureListener(ctx, *alb.LoadBalancerArn, *tg.TargetGroupArn, port); err != nil {
			return "", fmt.Errorf("ensure listener (port %d): %w", port.Port, err)
		}
	}

	return aws.ToString(alb.DNSName), nil
}

// findALB returns the ALB tagged with ownerTag, or nil if none exists.
func (p *albProvider) findALB(ctx context.Context, ownerTag string) (*types.LoadBalancer, error) {
	out, err := p.client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{})
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

	tagsOut, err := p.client.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: arns})
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

func (p *albProvider) createALB(ctx context.Context, svc *corev1.Service, ownerTag string) (*types.LoadBalancer, error) {
	subnets, err := requiredAnnotation(svc, annotationSubnets)
	if err != nil {
		return nil, err
	}
	securityGroups, err := requiredAnnotation(svc, annotationSecurityGroups)
	if err != nil {
		return nil, err
	}

	out, err := p.client.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{
		Name:           aws.String(albName(svc)),
		Subnets:        splitCSV(subnets),
		SecurityGroups: splitCSV(securityGroups),
		Scheme:         types.LoadBalancerSchemeEnumInternetFacing,
		Tags:           []types.Tag{{Key: aws.String(tagOwnerKey), Value: aws.String(ownerTag)}},
	})
	if err != nil {
		return nil, err
	}
	return &out.LoadBalancers[0], nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Target groups
// ──────────────────────────────────────────────────────────────────────────────

// findTargetGroup returns the target group for this service+port, or nil.
func (p *albProvider) findTargetGroup(ctx context.Context, ownerTag string, servicePort int32) (*types.TargetGroup, error) {
	out, err := p.client.DescribeTargetGroups(ctx, &elbv2.DescribeTargetGroupsInput{})
	if err != nil {
		return nil, err
	}
	if len(out.TargetGroups) == 0 {
		return nil, nil
	}

	arns := make([]string, 0, len(out.TargetGroups))
	for _, tg := range out.TargetGroups {
		arns = append(arns, *tg.TargetGroupArn)
	}

	tagsOut, err := p.client.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: arns})
	if err != nil {
		return nil, err
	}

	portTag := fmt.Sprintf("%s:%d", ownerTag, servicePort)
	for _, td := range tagsOut.TagDescriptions {
		for _, t := range td.Tags {
			if aws.ToString(t.Key) == tagOwnerKey && aws.ToString(t.Value) == portTag {
				for _, tg := range out.TargetGroups {
					if aws.ToString(tg.TargetGroupArn) == aws.ToString(td.ResourceArn) {
						return &tg, nil
					}
				}
			}
		}
	}
	return nil, nil
}

func (p *albProvider) createTargetGroup(ctx context.Context, svc *corev1.Service, ownerTag string, port corev1.ServicePort) (*types.TargetGroup, error) {
	vpcID, err := requiredAnnotation(svc, "samrajan.me/vpc-id")
	if err != nil {
		return nil, err
	}

	portTag := fmt.Sprintf("%s:%d", ownerTag, port.Port)
	out, err := p.client.CreateTargetGroup(ctx, &elbv2.CreateTargetGroupInput{
		Name:       aws.String(tgName(svc, port.Port)),
		Protocol:   types.ProtocolEnumHttp,
		Port:       aws.Int32(port.TargetPort.IntVal),
		TargetType: types.TargetTypeEnumIp,
		VpcId:      aws.String(vpcID),
		Tags:       []types.Tag{{Key: aws.String(tagOwnerKey), Value: aws.String(portTag)}},
	})
	if err != nil {
		return nil, err
	}
	return &out.TargetGroups[0], nil
}

// syncTargets deregisters stale targets and registers current ones.
func (p *albProvider) syncTargets(ctx context.Context, tgARN string, desired []string, targetPort int) error {
	healthOut, err := p.client.DescribeTargetHealth(ctx, &elbv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(tgARN),
	})
	if err != nil {
		return err
	}

	current := make(map[string]struct{}, len(healthOut.TargetHealthDescriptions))
	for _, t := range healthOut.TargetHealthDescriptions {
		current[aws.ToString(t.Target.Id)] = struct{}{}
	}

	desiredSet := make(map[string]struct{}, len(desired))
	for _, ip := range desired {
		desiredSet[ip] = struct{}{}
	}

	var toAdd, toRemove []types.TargetDescription
	for ip := range desiredSet {
		if _, ok := current[ip]; !ok {
			toAdd = append(toAdd, types.TargetDescription{Id: aws.String(ip), Port: aws.Int32(int32(targetPort))})
		}
	}
	for ip := range current {
		if _, ok := desiredSet[ip]; !ok {
			toRemove = append(toRemove, types.TargetDescription{Id: aws.String(ip)})
		}
	}

	if len(toRemove) > 0 {
		if _, err := p.client.DeregisterTargets(ctx, &elbv2.DeregisterTargetsInput{
			TargetGroupArn: aws.String(tgARN),
			Targets:        toRemove,
		}); err != nil {
			return err
		}
	}
	if len(toAdd) > 0 {
		if _, err := p.client.RegisterTargets(ctx, &elbv2.RegisterTargetsInput{
			TargetGroupArn: aws.String(tgARN),
			Targets:        toAdd,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Listeners
// ──────────────────────────────────────────────────────────────────────────────

func (p *albProvider) ensureListener(ctx context.Context, albARN, tgARN string, port corev1.ServicePort) error {
	listenersOut, err := p.client.DescribeListeners(ctx, &elbv2.DescribeListenersInput{
		LoadBalancerArn: aws.String(albARN),
	})
	if err != nil {
		return err
	}

	for _, l := range listenersOut.Listeners {
		if aws.ToInt32(l.Port) == port.Port {
			// Listener already exists; update its default action to point at tgARN.
			_, err := p.client.ModifyListener(ctx, &elbv2.ModifyListenerInput{
				ListenerArn: l.ListenerArn,
				DefaultActions: []types.Action{{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String(tgARN),
				}},
			})
			return err
		}
	}

	_, err = p.client.CreateListener(ctx, &elbv2.CreateListenerInput{
		LoadBalancerArn: aws.String(albARN),
		Protocol:        types.ProtocolEnumHttp,
		Port:            aws.Int32(port.Port),
		DefaultActions: []types.Action{{
			Type:           types.ActionTypeEnumForward,
			TargetGroupArn: aws.String(tgARN),
		}},
	})
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// DeleteLB
// ──────────────────────────────────────────────────────────────────────────────

func (p *albProvider) DeleteLB(ctx context.Context, svc *corev1.Service) error {
	ownerTag := ownerTagValue(svc)

	alb, err := p.findALB(ctx, ownerTag)
	if err != nil {
		return fmt.Errorf("find ALB: %w", err)
	}
	if alb == nil {
		return nil // already gone
	}

	// Delete listeners first (required before deleting the ALB).
	listenersOut, err := p.client.DescribeListeners(ctx, &elbv2.DescribeListenersInput{
		LoadBalancerArn: alb.LoadBalancerArn,
	})
	if err != nil {
		return fmt.Errorf("describe listeners: %w", err)
	}
	for _, l := range listenersOut.Listeners {
		if _, err := p.client.DeleteListener(ctx, &elbv2.DeleteListenerInput{
			ListenerArn: l.ListenerArn,
		}); err != nil {
			return fmt.Errorf("delete listener %s: %w", aws.ToString(l.ListenerArn), err)
		}
	}

	if _, err := p.client.DeleteLoadBalancer(ctx, &elbv2.DeleteLoadBalancerInput{
		LoadBalancerArn: alb.LoadBalancerArn,
	}); err != nil {
		return fmt.Errorf("delete ALB: %w", err)
	}

	// Delete target groups after the ALB (they cannot be deleted while in use).
	for _, port := range svc.Spec.Ports {
		tg, err := p.findTargetGroup(ctx, ownerTag, port.Port)
		if err != nil || tg == nil {
			continue
		}
		if _, err := p.client.DeleteTargetGroup(ctx, &elbv2.DeleteTargetGroupInput{
			TargetGroupArn: tg.TargetGroupArn,
		}); err != nil {
			return fmt.Errorf("delete target group (port %d): %w", port.Port, err)
		}
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func ownerTagValue(svc *corev1.Service) string {
	return svc.Namespace + "/" + svc.Name
}

// albName returns a name ≤32 chars derived from namespace and service name.
func albName(svc *corev1.Service) string {
	name := fmt.Sprintf("%s-%s", svc.Namespace, svc.Name)
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}

// tgName returns a name ≤32 chars for a target group.
func tgName(svc *corev1.Service, port int32) string {
	name := fmt.Sprintf("%s-%s-%d", svc.Namespace, svc.Name, port)
	if len(name) > 32 {
		name = name[:32]
	}
	return name
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

func requiredAnnotation(svc *corev1.Service, key string) (string, error) {
	v, ok := svc.Annotations[key]
	if !ok || v == "" {
		return "", fmt.Errorf("service %s/%s is missing required annotation %q", svc.Namespace, svc.Name, key)
	}
	return v, nil
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
