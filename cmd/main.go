/*
Copyright 2026 The external-lb-controller Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"os"

	// Register Kubernetes client auth plugins (Azure, GCP, OIDC, ...).
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/sam-rajan/external-lb-controller/internal/controller"
	"github.com/sam-rajan/external-lb-controller/internal/lb"
	_ "github.com/sam-rajan/external-lb-controller/internal/lb/aws" // register AWS ELBv2 provider
	servicewebhook "github.com/sam-rajan/external-lb-controller/internal/webhook/service"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func main() {
	cfg := parseFlags()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&cfg.ZapOptions)))

	webhookServer := buildWebhookServer(cfg)
	metricsServerOptions := buildMetricsOptions(cfg)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: cfg.ProbeAddr,
		LeaderElection:         cfg.EnableLeaderElection,
		LeaderElectionID:       "c0db1dc7.external-lb.io",
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	lbManager, err := lb.NewManager(context.Background(), lb.ProviderConfig{
		ClusterName:   cfg.ClusterName,
		CloudProvider: cfg.CloudProvider,
		K8sClient:     mgr.GetClient(),
	})
	if err != nil {
		setupLog.Error(err, "Failed to detect load balancer provider")
		os.Exit(1)
	}

	if err := (&controller.ServiceReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		LBManager: lbManager,
		Recorder:  mgr.GetEventRecorder("external-lb-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "service")
		os.Exit(1)
	}

	if err := servicewebhook.SetupServiceValidatorWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to set up service validator webhook")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}

// tlsOptions disables HTTP/2 unless enabled, to avoid the Rapid Reset and Stream
// Cancellation attacks (CVE-2023-44487, CVE-2023-39325).
func tlsOptions(cfg *Config) []func(*tls.Config) {
	if cfg.EnableHTTP2 {
		return nil
	}
	return []func(*tls.Config){
		func(c *tls.Config) {
			setupLog.Info("Disabling HTTP/2")
			c.NextProtos = []string{"http/1.1"}
		},
	}
}

func buildWebhookServer(cfg *Config) webhook.Server {
	opts := webhook.Options{TLSOpts: tlsOptions(cfg)}

	if cfg.WebhookCertDir != "" {
		setupLog.Info("Using provided webhook certificates", "path", cfg.WebhookCertDir)
		opts.CertDir = cfg.WebhookCertDir
		opts.CertName = cfg.WebhookCertName
		opts.KeyName = cfg.WebhookCertKey
	}

	return webhook.NewServer(opts)
}

func buildMetricsOptions(cfg *Config) metricsserver.Options {
	opts := metricsserver.Options{
		BindAddress:   cfg.MetricsAddr,
		SecureServing: cfg.SecureMetrics,
		TLSOpts:       tlsOptions(cfg),
	}

	if cfg.SecureMetrics {
		opts.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if cfg.MetricsCertDir != "" {
		setupLog.Info("Using provided metrics certificates", "path", cfg.MetricsCertDir)
		opts.CertDir = cfg.MetricsCertDir
		opts.CertName = cfg.MetricsCertName
		opts.KeyName = cfg.MetricsCertKey
	}

	return opts
}
