package aws

import "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

const (
	lbTypeNLB = "nlb"

	nlbType = types.LoadBalancerTypeEnumNetwork

	nlbBackendProtocol = types.ProtocolEnumTcp

	nlbSSLProtocol = types.ProtocolEnumTls
)
