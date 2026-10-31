package aws

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

func TestSSLEnabledForPort(t *testing.T) {
	tests := []struct {
		name  string
		ports string
		port  int32
		want  bool
	}{
		{"unset means all", "", 443, true},
		{"star means all", "*", 8443, true},
		{"listed", "443,8443", 8443, true},
		{"listed with spaces", " 443 , 8443 ", 443, true},
		{"not listed", "443", 80, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := svcWith(map[string]string{annotationSSLPorts: tt.ports})
			if got := sslEnabledForPort(svc, tt.port); got != tt.want {
				t.Errorf("sslEnabledForPort(%q, %d) = %v, want %v", tt.ports, tt.port, got, tt.want)
			}
		})
	}
}

func TestListenerProtocolAndCerts(t *testing.T) {
	const certA, certB = "arn:aws:acm:us-east-1:111122223333:certificate/a", "arn:aws:acm:us-east-1:111122223333:certificate/b"

	t.Run("no cert falls back to plaintext", func(t *testing.T) {
		proto, certs := listenerProtocolAndCerts(svcWith(nil), 80)
		if proto != types.ProtocolEnumTcp || len(certs) != 0 {
			t.Errorf("got (%v, %v), want (TCP, none)", proto, certs)
		}
	})

	t.Run("NLB with cert uses TLS", func(t *testing.T) {
		svc := svcWith(map[string]string{annotationSSLCert: certA + "," + certB})
		proto, certs := listenerProtocolAndCerts(svc, 443)
		if proto != types.ProtocolEnumTls {
			t.Errorf("protocol = %v, want TLS", proto)
		}
		if len(certs) != 2 || *certs[0].CertificateArn != certA || *certs[1].CertificateArn != certB {
			t.Errorf("certs = %v, want [%s %s] in order", certs, certA, certB)
		}
	})

	t.Run("ALB with cert uses HTTPS", func(t *testing.T) {
		svc := svcWith(map[string]string{annotationSSLCert: certA, annotationLBType: "alb"})
		if proto, _ := listenerProtocolAndCerts(svc, 443); proto != types.ProtocolEnumHttps {
			t.Errorf("protocol = %v, want HTTPS", proto)
		}
	})

	t.Run("port outside ssl-ports stays plaintext", func(t *testing.T) {
		svc := svcWith(map[string]string{annotationSSLCert: certA, annotationSSLPorts: "443"})
		proto, certs := listenerProtocolAndCerts(svc, 80)
		if proto != types.ProtocolEnumTcp || len(certs) != 0 {
			t.Errorf("got (%v, %v), want (TCP, none)", proto, certs)
		}
	})
}
