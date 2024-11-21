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
	"os"
	"path/filepath"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	apimacherrors "k8s.io/apimachinery/pkg/api/errors"
	apimachv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	//
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/certificates"
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

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var err error

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool

	// Custom flags from here
	var webhooksClientHostname string
	var webhooksClientPort int
	var webhooksClientTimeout int

	var webhooksServerPort int
	var webhooksServerPath string
	var webhooksServerCA string
	var webhooksServerCertificate string
	var webhooksServerPrivateKey string

	var webhooksServerAutogenerateCerts bool
	var webhooksServerCertsSecretName string

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

	// Custom flags from here
	flag.StringVar(&webhooksClientHostname, "webhook-client-hostname", "webhooks.admitik.svc",
		"The hostname used by Kubernetes when calling the webhooks server")
	flag.IntVar(&webhooksClientPort, "webhook-client-port", 10250,
		"The port used by Kubernetes when calling the webhooks server")
	flag.IntVar(&webhooksClientTimeout, "webhook-client-timeout", 10,
		"The time waited by Kubernetes when calling the webhooks server before considering timeout")

	flag.IntVar(&webhooksServerPort, "webhook-server-port", 10250,
		"The port where the webhooks server listens")
	flag.StringVar(&webhooksServerPath, "webhook-server-path", "/validate",
		"The path where the webhooks server listens")
	flag.StringVar(&webhooksServerCA, "webhook-server-ca", "",
		"The CA bundle to use for the webhooks server")
	flag.StringVar(&webhooksServerCertificate, "webhook-server-certificate", "",
		"The Certificate used by webhooks server")
	flag.StringVar(&webhooksServerPrivateKey, "webhook-server-private-key", "",
		"The Private Key used by webhooks server")

	flag.BoolVar(&webhooksServerAutogenerateCerts, "webhook-server-autogenerate-certs", false,
		"Enable autogeneration of certificates for webhooks server")
	flag.StringVar(&webhooksServerCertsSecretName, "webhook-server-certs-secret-name", "",
		"Kubernetes Secret object name to get certificates from. Keys are: ca.crt, tls.crt, tls.key")

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

	// Define the context separated as it will be used by our custom controller too.
	// This will synchronize goroutine death when the main controller is killed
	globals.Application.Context = ctrl.SetupSignalHandler()

	// Create ans store raw Kubernetes clients from client-go
	// They are used by kubebuilder non-related processess and controllers
	globals.Application.KubeRawClient, globals.Application.KubeRawCoreClient, err = globals.NewKubernetesClient()
	if err != nil {
		setupLog.Error(err, "unable to set up kubernetes clients")
		os.Exit(1)
	}

	///////////////////////////////////
	// Get/Generate certificates needed by admission webhooks server
	var ca, cert, privKey string

	if (webhooksServerCA != "" || webhooksServerCertificate != "" || webhooksServerPrivateKey != "") &&
		(webhooksServerCertsSecretName != "") {
		setupLog.Error(err, "getting certificates from files and from Secret objects are mutually exclusive")
		os.Exit(1)
	}

	currentNamespace, err := globals.GetCurrentNamespace()
	if err != nil {
		setupLog.Error(err, "unable to get current namespace")
		os.Exit(1)
	}

	if webhooksServerCertsSecretName != "" {

		succeededProcess := false
		for try := 0; try < 3; try++ {

			secretObj := &corev1.Secret{}
			secretObj, err = globals.Application.KubeRawCoreClient.CoreV1().Secrets(currentNamespace).
				Get(globals.Application.Context, webhooksServerCertsSecretName, apimachv1.GetOptions{})

			if err != nil {
				if !apimacherrors.IsNotFound(err) {
					setupLog.Error(err, "unable to get secret with certificates")
					continue
				}

				if apimacherrors.IsNotFound(err) && !webhooksServerAutogenerateCerts {
					setupLog.Error(err, "unable to get secret and autogeneration is disabled")
					continue
				}

				dnsNames := []string{"localhost", webhooksClientHostname}
				if strings.HasSuffix(webhooksClientHostname, ".cluster.local") {
					dnsNames = append(dnsNames, strings.TrimSuffix(webhooksClientHostname, ".cluster.local"))
				}
				if strings.HasSuffix(webhooksClientHostname, ".svc") {
					dnsNames = append(dnsNames, webhooksClientHostname+".cluster.local")
				}
				ca, cert, privKey, err = certificates.GenerateCerts(dnsNames)

				if err != nil {
					setupLog.Error(err, "unable to generate self-signed certificates")
					continue
				}

				secretObj.StringData = map[string]string{
					"ca.crt":  ca,
					"tls.crt": cert,
					"tls.key": privKey,
				}

				secretObj.Name = webhooksServerCertsSecretName
				_, err = globals.Application.KubeRawCoreClient.CoreV1().Secrets(currentNamespace).
					Create(globals.Application.Context, secretObj, apimachv1.CreateOptions{})
				if err != nil {
					setupLog.Error(err, "unable to create secret with self-signed certificates")
					continue
				}

				succeededProcess = true
				break
			}

			caBytes, dataFound := secretObj.Data["ca.crt"]
			if !dataFound {
				setupLog.Error(err, "unable to get ca.crt from defined secret")
				os.Exit(1)
			}
			ca = string(caBytes)

			certBytes, dataFound := secretObj.Data["tls.crt"]
			if !dataFound {
				setupLog.Error(err, "unable to get tls.crt from defined secret")
				os.Exit(1)
			}
			cert = string(certBytes)

			privKeyBytes, dataFound := secretObj.Data["tls.key"]
			if !dataFound {
				setupLog.Error(err, "unable to get tls.key from defined secret")
				os.Exit(1)
			}
			privKey = string(privKeyBytes)

			succeededProcess = true
			break
		}

		if !succeededProcess {
			setupLog.Error(err, "unable to get self-signed certificates")
			os.Exit(1)
		}

		tempDir := os.TempDir()
		webhooksServerCA = filepath.Join(tempDir, "ca.crt")
		os.WriteFile(webhooksServerCA, []byte(ca), 0744)

		webhooksServerCertificate = filepath.Join(tempDir, "tls.crt")
		os.WriteFile(webhooksServerCertificate, []byte(cert), 0744)

		webhooksServerPrivateKey = filepath.Join(tempDir, "tls.key")
		os.WriteFile(webhooksServerPrivateKey, []byte(privKey), 0744)
	}
	//////////////////////////////////

	// Load the CA bundle if defined
	caBundleBytes := []byte{}
	if webhooksServerCA != "" {
		caBundleBytes, err = os.ReadFile(webhooksServerCA)
		if err != nil {
			setupLog.Error(err, "unable to load CA bundle")
			os.Exit(1)
		}
	}

	// Generate WebhooksClientConfig with data passed by the user
	cfgWebhooksClientPort := webhooksServerPort
	if webhooksClientPort != 0 {
		cfgWebhooksClientPort = webhooksClientPort
	}
	webhookClientConfig, err := controller.GetWebhookClientConfig(caBundleBytes,
		webhooksClientHostname, cfgWebhooksClientPort, webhooksServerPath)
	if err != nil {
		setupLog.Error(err, "failed generating webhooks client config: %s", err.Error())
		os.Exit(1)
	}

	// Init primary controller
	// ATTENTION: This controller may be replaced by a custom one in the future doing the same tasks
	// to simplify this project's dependencies and maintainability
	if err = (&controller.ClusterAdmissionPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Options: controller.ClusterAdmissionPolicyControllerOptions{
			WebhookClientConfig: *webhookClientConfig,
			WebhookTimeout:      webhooksClientTimeout,
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

	// Init secondary controller to process coming events
	workloadController := xyz.WorkloadController{
		Client: mgr.GetClient(),
		Options: xyz.WorkloadControllerOptions{

			//
			ServerAddr: "0.0.0.0",
			ServerPort: webhooksServerPort,
			ServerPath: webhooksServerPath,

			//
			TLSCertificate: webhooksServerCertificate,
			TLSPrivateKey:  webhooksServerPrivateKey,
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
