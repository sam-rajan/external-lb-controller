package aws

import "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

const (
	// lbTypeALB is the annotationLBType value selecting an Application Load
	// Balancer, opted into explicitly since it's Layer 7 (HTTP/HTTPS) rather
	// than the Layer 4 TCP passthrough the admission webhook's port
	// validation assumes.
	lbTypeALB = "alb"

	// albType is the ELBv2 Type for an Application Load Balancer.
	albType = types.LoadBalancerTypeEnumApplication

	// albBackendProtocol is the plaintext (non-SSL) target group / listener
	// protocol for an ALB: HTTP (Layer 7).
	albBackendProtocol = types.ProtocolEnumHttp

	// albSSLProtocol is the listener protocol used when an ACM cert is
	// configured (annotationSSLCert) for an ALB: TLS termination at the LB.
	albSSLProtocol = types.ProtocolEnumHttps
)
