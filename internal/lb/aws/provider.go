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
	annotationSubnets        = "service.beta.kubernetes.io/aws-load-balancer-subnets"
	annotationSecurityGroups = "service.beta.kubernetes.io/aws-load-balancer-security-groups"
	annotationScheme         = "service.beta.kubernetes.io/aws-load-balancer-scheme"
	annotationLBType         = "service.beta.kubernetes.io/aws-load-balancer-type"
	annotationLBName         = "service.beta.kubernetes.io/aws-load-balancer-name"
	annotationSSLCert        = "service.beta.kubernetes.io/aws-load-balancer-ssl-cert"
	annotationSSLPorts       = "service.beta.kubernetes.io/aws-load-balancer-ssl-ports"
	annotationTargetType     = "service.beta.kubernetes.io/aws-load-balancer-target-type"
	annotationLBAttributes   = "service.beta.kubernetes.io/aws-load-balancer-attributes"

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

	tagOwnerKey = "external-lb.io/service"
)

func init() {
	lb.Register("aws", detect)
}

// detect confirms usable AWS credentials with sts:GetCallerIdentity, which any principal can call;
// config.LoadDefaultConfig alone resolves credentials lazily and rarely errors.
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
