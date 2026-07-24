package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
)

// tgName returns a name ≤32 chars for a target group.
func tgName(svc *corev1.Service, port int32) string {
	name := fmt.Sprintf("%s-%s-%d", svc.Namespace, svc.Name, port)
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}

func (p *elbProvider) findTargetGroup(ctx context.Context, ownerTag string, servicePort int32) (*types.TargetGroup, error) {
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

func (p *elbProvider) createTargetGroup(ctx context.Context, svc *corev1.Service, ownerTag string, port corev1.ServicePort, subnetIDs []string) (*types.TargetGroup, error) {
	vpcID, err := p.vpcIDFromSubnet(ctx, subnetIDs[0])
	if err != nil {
		return nil, fmt.Errorf("resolve VPC ID from subnet: %w", err)
	}

	portTag := fmt.Sprintf("%s:%d", ownerTag, port.Port)
	out, err := p.elb.CreateTargetGroup(ctx, &elbv2.CreateTargetGroupInput{
		Name:       aws.String(tgName(svc, port.Port)),
		Protocol:   backendProtocol(svc),
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
func (p *elbProvider) syncTargets(ctx context.Context, tgARN string, desired []string, targetPort int) error {
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
