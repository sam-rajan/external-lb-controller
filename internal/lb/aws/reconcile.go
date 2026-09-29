package aws

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
)

// reconcileSubnets updates the load balancer's subnets to match desired, if
// they've drifted (e.g. annotationSubnets was edited after creation).
func (p *elbProvider) reconcileSubnets(ctx context.Context, elb *types.LoadBalancer, desired []string) error {
	current := make([]string, 0, len(elb.AvailabilityZones))
	for _, az := range elb.AvailabilityZones {
		if az.SubnetId != nil {
			current = append(current, *az.SubnetId)
		}
	}
	if subnetSetEqual(current, desired) {
		return nil
	}
	_, err := p.elb.SetSubnets(ctx, &elbv2.SetSubnetsInput{
		LoadBalancerArn: elb.LoadBalancerArn,
		Subnets:         desired,
	})
	return err
}

func subnetSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := make(map[string]struct{}, len(a))
	for _, s := range a {
		as[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := as[s]; !ok {
			return false
		}
	}
	return true
}

// reconcileLoadBalancerAttributes applies annotationLBAttributes, if set, to
// the load balancer. ModifyLoadBalancerAttributes only touches the attributes
// present in the call, so this is safe to call on every reconcile.
func (p *elbProvider) reconcileLoadBalancerAttributes(ctx context.Context, lbARN string, svc *corev1.Service) error {
	attrs := parseLBAttributes(svc.Annotations[annotationLBAttributes])
	if len(attrs) == 0 {
		return nil
	}
	_, err := p.elb.ModifyLoadBalancerAttributes(ctx, &elbv2.ModifyLoadBalancerAttributesInput{
		LoadBalancerArn: aws.String(lbARN),
		Attributes:      attrs,
	})
	return err
}

// parseLBAttributes parses a comma-separated "key=value,key=value" string
// (the same format used by aws-load-balancer-controller) into ELBv2
// LoadBalancerAttribute values.
func parseLBAttributes(v string) []types.LoadBalancerAttribute {
	var attrs []types.LoadBalancerAttribute
	for _, kv := range splitCSV(v) {
		k, val, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		attrs = append(attrs, types.LoadBalancerAttribute{
			Key:   aws.String(strings.TrimSpace(k)),
			Value: aws.String(strings.TrimSpace(val)),
		})
	}
	return attrs
}

// reconcileTargetGroupHealthCheck applies any of the annotationHealthCheck*
// annotations that are set to the target group. Fields left unset in the
// ModifyTargetGroup call are left unchanged by AWS, so only annotations that
// are actually present are included; the call is skipped entirely if none are set.
func (p *elbProvider) reconcileTargetGroupHealthCheck(ctx context.Context, tgARN string, svc *corev1.Service) error {
	input := &elbv2.ModifyTargetGroupInput{TargetGroupArn: aws.String(tgARN)}
	var changed bool

	if v := svc.Annotations[annotationHealthCheckProtocol]; v != "" {
		input.HealthCheckProtocol = types.ProtocolEnum(v)
		changed = true
	}
	if v := svc.Annotations[annotationHealthCheckPort]; v != "" {
		input.HealthCheckPort = aws.String(v)
		changed = true
	}
	if v := svc.Annotations[annotationHealthCheckPath]; v != "" {
		input.HealthCheckPath = aws.String(v)
		changed = true
	}
	if v, err := healthCheckInt32(svc, annotationHealthCheckIntervalSeconds); err != nil {
		return err
	} else if v != nil {
		input.HealthCheckIntervalSeconds = v
		changed = true
	}
	if v, err := healthCheckInt32(svc, annotationHealthCheckTimeoutSeconds); err != nil {
		return err
	} else if v != nil {
		input.HealthCheckTimeoutSeconds = v
		changed = true
	}
	if v, err := healthCheckInt32(svc, annotationHealthyThresholdCount); err != nil {
		return err
	} else if v != nil {
		input.HealthyThresholdCount = v
		changed = true
	}
	if v, err := healthCheckInt32(svc, annotationUnhealthyThresholdCount); err != nil {
		return err
	} else if v != nil {
		input.UnhealthyThresholdCount = v
		changed = true
	}

	if !changed {
		return nil
	}
	_, err := p.elb.ModifyTargetGroup(ctx, input)
	return err
}

func healthCheckInt32(svc *corev1.Service, annotation string) (*int32, error) {
	v := svc.Annotations[annotation]
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", annotation, err)
	}
	return aws.Int32(int32(n)), nil
}
