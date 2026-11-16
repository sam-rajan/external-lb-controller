package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sam-rajan/external-lb-controller/internal/lb"
)

const (
	// Standard AWS load balancer annotations (compatible with aws-load-balancer-controller).
	annotationSubnets        = "service.beta.kubernetes.io/aws-load-balancer-subnets"
	annotationSecurityGroups = "service.beta.kubernetes.io/aws-load-balancer-security-groups"
	annotationScheme         = "service.beta.kubernetes.io/aws-load-balancer-scheme"      // "internet-facing" (default) or "internal"
	annotationLBType         = "service.beta.kubernetes.io/aws-load-balancer-type"        // "nlb" (default) or "alb"
	annotationLBName         = "service.beta.kubernetes.io/aws-load-balancer-name"        // custom name, defaults to "<namespace>-<name>"
	annotationSSLCert        = "service.beta.kubernetes.io/aws-load-balancer-ssl-cert"    // comma-separated ACM cert ARNs; first is the default cert
	annotationSSLPorts       = "service.beta.kubernetes.io/aws-load-balancer-ssl-ports"   // comma-separated ports, or "*"/absent for all ports (only meaningful if ssl-cert is set)
	annotationTargetType     = "service.beta.kubernetes.io/aws-load-balancer-target-type" // "instance" (default) or "ip"
	annotationLBAttributes   = "service.beta.kubernetes.io/aws-load-balancer-attributes"  // comma-separated key=value ELBv2 load balancer attributes, e.g. "load_balancing.cross_zone.enabled=true,deletion_protection.enabled=true"

	// Target group health check annotations; each is optional and left as the
	// ELBv2 default when unset.
	annotationHealthCheckProtocol        = "service.beta.kubernetes.io/aws-load-balancer-healthcheck-protocol"
	annotationHealthCheckPort            = "service.beta.kubernetes.io/aws-load-balancer-healthcheck-port"
	annotationHealthCheckPath            = "service.beta.kubernetes.io/aws-load-balancer-healthcheck-path"
	annotationHealthCheckIntervalSeconds = "service.beta.kubernetes.io/aws-load-balancer-healthcheck-interval-seconds"
	annotationHealthCheckTimeoutSeconds  = "service.beta.kubernetes.io/aws-load-balancer-healthcheck-timeout-seconds"
	annotationHealthyThresholdCount      = "service.beta.kubernetes.io/aws-load-balancer-healthy-threshold-count"
	annotationUnhealthyThresholdCount    = "service.beta.kubernetes.io/aws-load-balancer-unhealthy-threshold-count"

	targetTypeInstance = "instance"
	targetTypeIP       = "ip"

	schemeInternetFacing = "internet-facing"
	schemeInternal       = "internal"

	// Tag applied to every AWS resource managed by this controller.
	tagOwnerKey = "external-lb.io/service"
)

func init() {
	lb.Register("aws", detect)
}

// detect verifies the controller has working AWS credentials by calling
// sts:GetCallerIdentity, an action every principal can call regardless of its
// permissions. Unlike config.LoadDefaultConfig alone (which resolves
// credentials lazily and rarely errors up front), this actually confirms the
// credentials are usable before the AWS provider is selected.
func detect(ctx context.Context, providerCfg lb.ProviderConfig) (lb.Provider, bool) {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, false
	}
	if _, err := sts.NewFromConfig(awsCfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err != nil {
		return nil, false
	}
	return &elbProvider{
		elb:         elbv2.NewFromConfig(awsCfg),
		ec2:         ec2.NewFromConfig(awsCfg),
		clusterName: providerCfg.ClusterName,
		k8sClient:   providerCfg.K8sClient,
	}, true
}

type elbProvider struct {
	elb         *elbv2.Client
	ec2         *ec2.Client
	clusterName string
	k8sClient   client.Client
}
