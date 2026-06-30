// Package aws registers an AWS ALB-backed load balancer provider.
package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
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
	annotationScheme         = "service.beta.kubernetes.io/aws-load-balancer-scheme" // "internet-facing" (default) or "internal"

	// Tag applied to every AWS resource managed by this controller.
	tagOwnerKey = "otlbc.io/service"
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
		elb: elbv2.NewFromConfig(cfg),
		ec2: ec2.NewFromConfig(cfg),
	}, true
}

type albProvider struct {
	elb *elbv2.Client
	ec2 *ec2.Client
}

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

func (p *albProvider) createALB(ctx context.Context, svc *corev1.Service, ownerTag string) (*types.LoadBalancer, error) {
	subnets, err := requiredAnnotation(svc, annotationSubnets)
	if err != nil {
		return nil, err
	}
	subnetIDs := splitCSV(subnets)

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
		sgIDs = []string{sgID}
	}

	scheme := types.LoadBalancerSchemeEnumInternetFacing
	if svc.Annotations[annotationScheme] == "internal" {
		scheme = types.LoadBalancerSchemeEnumInternal
	}

	out, err := p.elb.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{
		Name:           aws.String(albName(svc)),
		Subnets:        subnetIDs,
		SecurityGroups: sgIDs,
		Scheme:         scheme,
		Tags:           []types.Tag{{Key: aws.String(tagOwnerKey), Value: aws.String(ownerTag)}},
	})
	if err != nil {
		return nil, err
	}
	return &out.LoadBalancers[0], nil
}

// ensureSecurityGroup finds the auto-managed SG for this service, or creates one.
func (p *albProvider) ensureSecurityGroup(ctx context.Context, svc *corev1.Service, ownerTag, vpcID string) (string, error) {
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
		GroupName:   aws.String(albName(svc)),
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

func (p *albProvider) findTargetGroup(ctx context.Context, ownerTag string, servicePort int32) (*types.TargetGroup, error) {
	out, err := p.elb.DescribeTargetGroups(ctx, &elbv2.DescribeTargetGroupsInput{})
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

	tagsOut, err := p.elb.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: arns})
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
	subnets, err := requiredAnnotation(svc, annotationSubnets)
	if err != nil {
		return nil, err
	}
	vpcID, err := p.vpcIDFromSubnet(ctx, splitCSV(subnets)[0])
	if err != nil {
		return nil, fmt.Errorf("resolve VPC ID from subnet: %w", err)
	}

	portTag := fmt.Sprintf("%s:%d", ownerTag, port.Port)
	out, err := p.elb.CreateTargetGroup(ctx, &elbv2.CreateTargetGroupInput{
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
	healthOut, err := p.elb.DescribeTargetHealth(ctx, &elbv2.DescribeTargetHealthInput{
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
		if _, err := p.elb.DeregisterTargets(ctx, &elbv2.DeregisterTargetsInput{
			TargetGroupArn: aws.String(tgARN),
			Targets:        toRemove,
		}); err != nil {
			return err
		}
	}
	if len(toAdd) > 0 {
		if _, err := p.elb.RegisterTargets(ctx, &elbv2.RegisterTargetsInput{
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
	listenersOut, err := p.elb.DescribeListeners(ctx, &elbv2.DescribeListenersInput{
		LoadBalancerArn: aws.String(albARN),
	})
	if err != nil {
		return err
	}

	for _, l := range listenersOut.Listeners {
		if aws.ToInt32(l.Port) == port.Port {
			// Listener already exists; update its default action to point at tgARN.
			_, err := p.elb.ModifyListener(ctx, &elbv2.ModifyListenerInput{
				ListenerArn: l.ListenerArn,
				DefaultActions: []types.Action{{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String(tgARN),
				}},
			})
			return err
		}
	}

	_, err = p.elb.CreateListener(ctx, &elbv2.CreateListenerInput{
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
	listenersOut, err := p.elb.DescribeListeners(ctx, &elbv2.DescribeListenersInput{
		LoadBalancerArn: alb.LoadBalancerArn,
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
		LoadBalancerArn: alb.LoadBalancerArn,
	}); err != nil {
		return fmt.Errorf("delete ALB: %w", err)
	}

	// Wait for the ALB to be fully deleted before releasing dependent resources.
	// AWS deletes ALBs asynchronously; SG and target groups cannot be removed while
	// the ALB still holds references to them.
	waiter := elbv2.NewLoadBalancersDeletedWaiter(p.elb)
	if err := waiter.Wait(ctx, &elbv2.DescribeLoadBalancersInput{
		LoadBalancerArns: []string{aws.ToString(alb.LoadBalancerArn)},
	}, 5*time.Minute); err != nil {
		return fmt.Errorf("wait for ALB deletion: %w", err)
	}

	// Delete target groups after the ALB (they cannot be deleted while in use).
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
	if err := p.deleteMangedSecurityGroup(ctx, ownerTag, alb); err != nil {
		return fmt.Errorf("delete managed security group: %w", err)
	}

	return nil
}

func (p *albProvider) deleteMangedSecurityGroup(ctx context.Context, ownerTag string, alb *types.LoadBalancer) error {
	vpcID := ""
	for _, az := range alb.AvailabilityZones {
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

func (p *albProvider) deleteSecurityGroupWithRetry(ctx context.Context, sgID string) error {
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

func (p *albProvider) vpcIDFromSubnet(ctx context.Context, subnetID string) (string, error) {
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
