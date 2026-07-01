package cert

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"sync"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	certValidity    = 365 * 24 * time.Hour
	rotateThreshold = 30 * 24 * time.Hour
	rotationCheck   = 1 * time.Hour

	secretKeyCA   = "ca.crt"
	secretKeyCert = "tls.crt"
	secretKeyKey  = "tls.key"
)

// Options configures the cert Manager.
type Options struct {
	// SecretName is the Secret used to persist certs across pod restarts.
	SecretName string
	// Namespace is where the Secret lives (typically the controller's namespace).
	Namespace string
	// ServiceName is the Kubernetes Service fronting the webhook server.
	// Used to build the cert's SAN: <service>.<namespace>.svc[.cluster.local].
	ServiceName string
	// WebhookConfigName is the ValidatingWebhookConfiguration to keep caBundle in sync on.
	WebhookConfigName string
}

// Manager handles webhook TLS cert lifecycle: generation, Secret persistence,
// caBundle patching, and background rotation.
type Manager struct {
	opts   Options
	client client.Client
	mu     sync.RWMutex
	cert   *tls.Certificate
}

// New bootstraps a Manager: loads an existing valid cert from the Secret or
// generates a fresh one, patches caBundle on the ValidatingWebhookConfiguration,
// and holds the cert in memory for the webhook server's GetCertificate callback.
func New(ctx context.Context, c client.Client, opts Options) (*Manager, error) {
	m := &Manager{opts: opts, client: c}
	if err := m.bootstrap(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// GetCertificate implements tls.Config.GetCertificate.
// Wire this into the webhook server's TLSOpts so rotation takes effect without
// a pod restart.
func (m *Manager) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cert, nil
}

// StartRotation runs a background goroutine that checks cert expiry every hour
// and rotates when less than 30 days remain.
func (m *Manager) StartRotation(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(rotationCheck)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !m.needsRotation() {
					continue
				}
				_ = m.bootstrap(ctx) // rotation failures are non-fatal; next tick retries
			}
		}
	}()
}

func (m *Manager) needsRotation() bool {
	m.mu.RLock()
	c := m.cert
	m.mu.RUnlock()
	if c == nil || len(c.Certificate) == 0 {
		return true
	}
	parsed, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return true
	}
	return time.Until(parsed.NotAfter) <= rotateThreshold
}

func (m *Manager) bootstrap(ctx context.Context) error {
	caPEM, certPEM, keyPEM, err := m.loadOrGenerate(ctx)
	if err != nil {
		return err
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("parsing cert keypair: %w", err)
	}

	m.mu.Lock()
	m.cert = &tlsCert
	m.mu.Unlock()

	return m.patchCABundle(ctx, caPEM)
}

// loadOrGenerate returns the existing cert from the Secret if still valid,
// otherwise generates a new CA + server cert pair and persists it.
func (m *Manager) loadOrGenerate(ctx context.Context) (caPEM, certPEM, keyPEM []byte, err error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: m.opts.SecretName, Namespace: m.opts.Namespace}
	getErr := m.client.Get(ctx, key, secret)

	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return nil, nil, nil, fmt.Errorf("getting cert secret: %w", getErr)
	}

	if getErr == nil {
		ca := secret.Data[secretKeyCA]
		crt := secret.Data[secretKeyCert]
		k := secret.Data[secretKeyKey]
		if stillValid(crt, ca) {
			return ca, crt, k, nil
		}
	}

	caPEM, certPEM, keyPEM, err = generate(m.dnsNames())
	if err != nil {
		return nil, nil, nil, err
	}

	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.opts.SecretName,
			Namespace: m.opts.Namespace,
		},
		Data: map[string][]byte{
			secretKeyCA:   caPEM,
			secretKeyCert: certPEM,
			secretKeyKey:  keyPEM,
		},
	}

	if apierrors.IsNotFound(getErr) {
		err = m.client.Create(ctx, desired)
	} else {
		secret.Data = desired.Data
		err = m.client.Update(ctx, secret)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("saving cert secret: %w", err)
	}
	return caPEM, certPEM, keyPEM, nil
}

func (m *Manager) patchCABundle(ctx context.Context, caPEM []byte) error {
	cfg := &admissionv1.ValidatingWebhookConfiguration{}
	if err := m.client.Get(ctx, types.NamespacedName{Name: m.opts.WebhookConfigName}, cfg); err != nil {
		return fmt.Errorf("getting ValidatingWebhookConfiguration %q: %w", m.opts.WebhookConfigName, err)
	}
	patch := client.MergeFrom(cfg.DeepCopy())
	for i := range cfg.Webhooks {
		cfg.Webhooks[i].ClientConfig.CABundle = caPEM
	}
	return m.client.Patch(ctx, cfg, patch)
}

func (m *Manager) dnsNames() []string {
	svc, ns := m.opts.ServiceName, m.opts.Namespace
	return []string{
		fmt.Sprintf("%s.%s.svc", svc, ns),
		fmt.Sprintf("%s.%s.svc.cluster.local", svc, ns),
	}
}

// stillValid returns true when the cert is parseable and has more than
// rotateThreshold left before expiry.
func stillValid(certPEM, caPEM []byte) bool {
	if len(certPEM) == 0 || len(caPEM) == 0 {
		return false
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	return time.Until(parsed.NotAfter) > rotateThreshold
}

// generate creates a self-signed CA and a server cert signed by that CA.
func generate(dnsNames []string) (caPEM, certPEM, keyPEM []byte, err error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating CA key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "otlbc-webhook-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(certValidity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating CA cert: %w", err)
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, nil, nil, err
	}

	srvKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating server key: %w", err)
	}
	srvTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: dnsNames[0]},
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(certValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTemplate, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating server cert: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(srvKey)})
	return caPEM, certPEM, keyPEM, nil
}
