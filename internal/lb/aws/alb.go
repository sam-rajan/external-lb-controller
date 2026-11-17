package aws

import "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

const (
	// ALB is opt-in: it is Layer 7, while the webhook's port validation assumes Layer 4.
	lbTypeALB = "alb"

	albType = types.LoadBalancerTypeEnumApplication

	albBackendProtocol = types.ProtocolEnumHttp

	albSSLProtocol = types.ProtocolEnumHttps
)
