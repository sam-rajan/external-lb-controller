package aws

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

func targetType(svc *corev1.Service) types.TargetTypeEnum {
	if svc.Annotations[annotationTargetType] == targetTypeIP {
		return types.TargetTypeEnumIp
	}
	return types.TargetTypeEnumInstance
}

func loadBalancerType(svc *corev1.Service) types.LoadBalancerTypeEnum {
	if svc.Annotations[annotationLBType] == lbTypeALB {
		return albType
	}
	return nlbType
}

func backendProtocol(svc *corev1.Service) types.ProtocolEnum {
	if svc.Annotations[annotationLBType] == lbTypeALB {
		return albBackendProtocol
	}
	return nlbBackendProtocol
}

func loadBalancerScheme(svc *corev1.Service) types.LoadBalancerSchemeEnum {
	if svc.Annotations[annotationScheme] == schemeInternal {
		return types.LoadBalancerSchemeEnumInternal
	}
	return types.LoadBalancerSchemeEnumInternetFacing
}
