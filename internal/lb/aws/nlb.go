package aws

import "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

const (
	// lbTypeNLB is the annotationLBType value selecting a Network Load
	// Balancer. It's the default when the annotation is unset, matching the
	// TCP-only port validation enforced by the admission webhook.
	lbTypeNLB = "nlb"

	// nlbType is the ELBv2 Type for a Network Load Balancer.
	nlbType = types.LoadBalancerTypeEnumNetwork

	// nlbBackendProtocol is the plaintext (non-SSL) target group / listener
	// protocol for an NLB: raw TCP passthrough (Layer 4).
	nlbBackendProtocol = types.ProtocolEnumTcp

	// nlbSSLProtocol is the listener protocol used when an ACM cert is
	// configured (annotationSSLCert) for an NLB: TLS termination at the LB.
	nlbSSLProtocol = types.ProtocolEnumTls
)
