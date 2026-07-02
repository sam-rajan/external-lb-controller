package main

import (
	"flag"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Config holds all command-line configuration for the controller manager.
type Config struct {
	MetricsAddr     string
	MetricsCertDir  string
	MetricsCertName string
	MetricsCertKey  string
	SecureMetrics   bool

	WebhookCertDir     string
	WebhookCertName    string
	WebhookCertKey     string
	WebhookSecretName  string
	WebhookServiceName string
	WebhookConfigName  string

	ProbeAddr            string
	EnableLeaderElection bool

	ZapOptions zap.Options
}

// parseFlags registers all flags and calls flag.Parse, returning the populated Config.
func parseFlags() *Config {
	cfg := &Config{}

	// Default to production logging (JSON, info level). Developers can opt into
	// human-readable debug output with --zap-devel.
	cfg.ZapOptions = zap.Options{Development: false}

	flag.StringVar(&cfg.MetricsAddr, "metrics-bind-address", "0",
		"The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable.")
	flag.BoolVar(&cfg.SecureMetrics, "metrics-secure", true,
		"Serve the metrics endpoint over HTTPS. Use --metrics-secure=false for HTTP.")
	flag.StringVar(&cfg.MetricsCertDir, "metrics-cert-path", "", "Directory containing the metrics server certificate.")
	flag.StringVar(&cfg.MetricsCertName, "metrics-cert-name", "tls.crt", "Metrics server certificate filename.")
	flag.StringVar(&cfg.MetricsCertKey, "metrics-cert-key", "tls.key", "Metrics server key filename.")

	flag.StringVar(&cfg.WebhookCertDir, "webhook-cert-path", "", "Directory containing the webhook certificate.")
	flag.StringVar(&cfg.WebhookCertName, "webhook-cert-name", "tls.crt", "Webhook certificate filename.")
	flag.StringVar(&cfg.WebhookCertKey, "webhook-cert-key", "tls.key", "Webhook key filename.")
	flag.StringVar(&cfg.WebhookSecretName, "webhook-secret-name", "otlbc-webhook-certs",
		"Secret used to persist generated webhook TLS certs.")
	flag.StringVar(&cfg.WebhookServiceName, "webhook-service-name", "otlbc-webhook-service",
		"Kubernetes Service fronting the webhook server (used for cert SANs).")
	flag.StringVar(&cfg.WebhookConfigName, "webhook-config-name", "",
		"Name of the ValidatingWebhookConfiguration to keep caBundle in sync. Leave empty to skip cert bootstrap.")

	flag.StringVar(&cfg.ProbeAddr, "health-probe-bind-address", ":8081", "The address the health probe endpoint binds to.")
	flag.BoolVar(&cfg.EnableLeaderElection, "leader-elect", false,
		"Enable leader election to ensure only one active controller manager.")

	cfg.ZapOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	return cfg
}
