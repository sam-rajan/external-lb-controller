package aws

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	corev1 "k8s.io/api/core/v1"
)

func (p *elbProvider) ensureListener(ctx context.Context, svc *corev1.Service, lbARN, tgARN string, port corev1.ServicePort) error {
	var listeners []types.Listener
	paginator := elbv2.NewDescribeListenersPaginator(p.elb, &elbv2.DescribeListenersInput{
		LoadBalancerArn: aws.String(lbARN),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		listeners = append(listeners, page.Listeners...)
	}

	protocol, certs := listenerProtocolAndCerts(svc, port.Port)
	var defaultCert []types.Certificate
	if len(certs) > 0 {
		defaultCert = certs[:1]
	}

	for _, l := range listeners {
		if aws.ToInt32(l.Port) == port.Port {
			// Listener already exists; reconcile its protocol/cert and default action.
			_, err := p.elb.ModifyListener(ctx, &elbv2.ModifyListenerInput{
				ListenerArn:  l.ListenerArn,
				Protocol:     protocol,
				Certificates: defaultCert,
				DefaultActions: []types.Action{{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String(tgARN),
				}},
			})
			if err != nil {
				return err
			}
			return p.reconcileListenerCertificates(ctx, aws.ToString(l.ListenerArn), certs)
		}
	}

	out, err := p.elb.CreateListener(ctx, &elbv2.CreateListenerInput{
		LoadBalancerArn: aws.String(lbARN),
		Protocol:        protocol,
		Port:            aws.Int32(port.Port),
		Certificates:    defaultCert,
		DefaultActions: []types.Action{{
			Type:           types.ActionTypeEnumForward,
			TargetGroupArn: aws.String(tgARN),
		}},
	})
	if err != nil {
		return err
	}
	if len(out.Listeners) == 0 {
		return nil
	}
	return p.reconcileListenerCertificates(ctx, aws.ToString(out.Listeners[0].ListenerArn), certs)
}

// listenerProtocolAndCerts resolves the listener protocol and ACM certs for
// a given Service port. SSL is enabled only if annotationSSLCert is set and
// the port is covered by annotationSSLPorts (absent/"*" means all ports);
// otherwise it falls back to the existing plaintext backendProtocol.
func listenerProtocolAndCerts(svc *corev1.Service, port int32) (types.ProtocolEnum, []types.Certificate) {
	certARNs := splitCSV(svc.Annotations[annotationSSLCert])
	if len(certARNs) == 0 || !sslEnabledForPort(svc, port) {
		return backendProtocol(svc), nil
	}

	certs := make([]types.Certificate, len(certARNs))
	for i, arn := range certARNs {
		certs[i] = types.Certificate{CertificateArn: aws.String(arn)}
	}

	if svc.Annotations[annotationLBType] == lbTypeALB {
		return albSSLProtocol, certs
	}
	return nlbSSLProtocol, certs
}

func sslEnabledForPort(svc *corev1.Service, port int32) bool {
	v := svc.Annotations[annotationSSLPorts]
	if v == "" || v == "*" {
		return true
	}
	return slices.Contains(splitCSV(v), strconv.Itoa(int(port)))
}

// reconcileListenerCertificates ensures the listener's additional (SNI)
// certificates match certs[1:] exactly (certs[0], if any, is the default
// certificate and is set directly on the listener, not here).
func (p *elbProvider) reconcileListenerCertificates(ctx context.Context, listenerARN string, certs []types.Certificate) error {
	var desired []types.Certificate
	if len(certs) > 1 {
		desired = certs[1:]
	}

	out, err := p.elb.DescribeListenerCertificates(ctx, &elbv2.DescribeListenerCertificatesInput{
		ListenerArn: aws.String(listenerARN),
	})
	if err != nil {
		return fmt.Errorf("describe listener certificates: %w", err)
	}

	desiredARNs := make(map[string]struct{}, len(desired))
	for _, c := range desired {
		desiredARNs[aws.ToString(c.CertificateArn)] = struct{}{}
	}

	currentARNs := make(map[string]struct{})
	for _, c := range out.Certificates {
		if aws.ToBool(c.IsDefault) {
			continue
		}
		currentARNs[aws.ToString(c.CertificateArn)] = struct{}{}
	}

	var toAdd, toRemove []types.Certificate
	for arn := range desiredARNs {
		if _, ok := currentARNs[arn]; !ok {
			toAdd = append(toAdd, types.Certificate{CertificateArn: aws.String(arn)})
		}
	}
	for arn := range currentARNs {
		if _, ok := desiredARNs[arn]; !ok {
			toRemove = append(toRemove, types.Certificate{CertificateArn: aws.String(arn)})
		}
	}

	if len(toRemove) > 0 {
		if _, err := p.elb.RemoveListenerCertificates(ctx, &elbv2.RemoveListenerCertificatesInput{
			ListenerArn:  aws.String(listenerARN),
			Certificates: toRemove,
		}); err != nil {
			return fmt.Errorf("remove stale listener certificates: %w", err)
		}
	}
	if len(toAdd) > 0 {
		if _, err := p.elb.AddListenerCertificates(ctx, &elbv2.AddListenerCertificatesInput{
			ListenerArn:  aws.String(listenerARN),
			Certificates: toAdd,
		}); err != nil {
			return fmt.Errorf("add listener certificates: %w", err)
		}
	}
	return nil
}
