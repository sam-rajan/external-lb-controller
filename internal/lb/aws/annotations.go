package aws

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// targetType resolves the ELBv2 target type requested via annotationTargetType,
// defaulting to "instance" (register nodes + NodePort; see node.go).
func targetType(svc *corev1.Service) types.TargetTypeEnum {
	if svc.Annotations[annotationTargetType] == targetTypeIP {
		return types.TargetTypeEnumIp
	}
	return types.TargetTypeEnumInstance
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
	if svc.Annotations[annotationScheme] == schemeInternal {
		return types.LoadBalancerSchemeEnumInternal
	}
	return types.LoadBalancerSchemeEnumInternetFacing
}
