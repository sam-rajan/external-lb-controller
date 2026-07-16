/*
Copyright 2026.

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
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
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

	"samrajan.me/otlbc/internal/controller"
	"samrajan.me/otlbc/internal/lb"
	_ "samrajan.me/otlbc/internal/lb/aws" // register AWS ALB provider
	otlbcwebhook "samrajan.me/otlbc/internal/webhook"
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
		LeaderElectionID:       "c0db1dc7.otlbc.io",
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	lbManager, err := lb.NewManager(context.Background())
	if err != nil {
		setupLog.Error(err, "Failed to detect load balancer provider")
		os.Exit(1)
	}

	if err := (&controller.ServiceReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		LBManager: lbManager,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "service")
		os.Exit(1)
	}

	if err := otlbcwebhook.SetupServiceValidatorWithManager(mgr); err != nil {
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

func buildWebhookServer(cfg *Config) webhook.Server {
	opts := webhook.Options{}

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
