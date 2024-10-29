/*
Copyright 2024.

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
	"crypto/tls"
	"flag"
	"fmt"
	"net/url"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	admissionV1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	admitikv1alpha1 "freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/controller"
	"freepik.com/admitik/internal/globals"
	"freepik.com/admitik/internal/xyz"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(admitikv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool

	// Custom flags from here
	var webhooksServerCABundle string
	var webhooksServerPort int
	var webhooksServerPath string
	var webhooksClientHostname string
	var webhooksClientPort int

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. "+
		"Use the port :8080. If not set, it will be 0 in order to disable the metrics server")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	flag.StringVar(&webhooksServerCABundle, "webhook-server-ca-bundle", "", "The CA bundle to use for the webhooks server")
	flag.IntVar(&webhooksServerPort, "webhook-server-port", 10250, "The port where the webhooks server listens")
	flag.StringVar(&webhooksServerPath, "webhook-server-path", "/validate", "The path where the webhooks server listens")

	flag.StringVar(&webhooksClientHostname, "webhook-client-hostname", "webhooks.admitik.svc", "The hostname used by Kubernetes when calling the webhooks server")
	flag.IntVar(&webhooksClientPort, "webhook-client-port", 10250, "The port used by Kubernetes when calling the webhooks server")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9ee19594.freepik.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// When 'webhook-client-port' flag is defined, configure Kubernetes to call webhooks there.
	// Otherwise, use port defined in 'webhooks-server-port'.
	// Being able to use different port for launching the webhooks server and the url in ValidatingWebhookConfiguration
	// allows us to test the webhooks server locally, through a reverse tunnel
	webhooksClientHost := fmt.Sprintf("%s:%d", webhooksClientHostname, webhooksServerPort)
	if webhooksClientPort != 0 {
		webhooksClientHost = fmt.Sprintf("%s:%d", webhooksClientHostname, webhooksClientPort)
	}

	webhooksServerUrl := url.URL{
		Scheme: "https",
		Host:   webhooksClientHost,
		Path:   webhooksServerPath,
	}

	if err = (&controller.ClusterAdmissionPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Options: controller.ClusterAdmissionPolicyControllerOptions{
			WebhookClientConfig: admissionV1.WebhookClientConfig{
				URL:      func(s string) *string { return &s }(webhooksServerUrl.String()),
				CABundle: []byte(webhooksServerCABundle),
			},
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterAdmissionPolicy")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Define the context separated as it will be used by our custom controller too.
	// This will synchronize goroutine death when the main controller is killed
	//signalCtx := ctrl.SetupSignalHandler()
	globals.Application.Context = ctrl.SetupSignalHandler()
	globals.Application.KubeRawClient, err = globals.NewKubernetesClient()
	if err != nil {
		setupLog.Error(err, "unable to set up kubernetes client")
		os.Exit(1)
	}

	// Init secondary controller to process coming events
	workloadController := xyz.WorkloadController{
		Client: mgr.GetClient(),
		Options: xyz.WorkloadControllerOptions{
			ServerAddr:     "0.0.0.0",                      // TODO: Get this from flags
			ServerPort:     webhooksServerPort,             // TODO: Get this from flags
			ServerPath:     webhooksServerPath,             // TODO: Get this from flags
			ServerCaBundle: []byte(webhooksServerCABundle), // TODO: Get this from flags
		},
	}

	setupLog.Info("starting workload controller")
	go workloadController.Start(globals.Application.Context)

	//
	setupLog.Info("starting manager")
	if err := mgr.Start(globals.Application.Context); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
