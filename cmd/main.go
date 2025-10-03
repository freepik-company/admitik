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
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	apimacherrors "k8s.io/apimachinery/pkg/api/errors"
	apimachv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/certificates"
	"github.com/freepik-company/admitik/internal/controller"
	"github.com/freepik-company/admitik/internal/controller/clustergenerationpolicy"
	"github.com/freepik-company/admitik/internal/controller/clustermutationpolicy"
	"github.com/freepik-company/admitik/internal/controller/clustervalidationpolicy"
	"github.com/freepik-company/admitik/internal/controller/observedresource"
	"github.com/freepik-company/admitik/internal/controller/sources"
	"github.com/freepik-company/admitik/internal/globals"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	resourceInformerRegistry "github.com/freepik-company/admitik/internal/registry/resourceinformer"
	resourceObserverRegistry "github.com/freepik-company/admitik/internal/registry/resourceobserver"
	sourcesRegistry "github.com/freepik-company/admitik/internal/registry/sources"
	"github.com/freepik-company/admitik/internal/server/admission"
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
	var sourcesTimeToResyncInformers time.Duration

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

	var kubeClientQps float64
	var kubeClientBurst int

	var enableSpecialLabels bool
	var excludeAdmissionSelfNamespace bool
	var excludedAdmissionNamespaces string

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
	flag.DurationVar(&sourcesTimeToResyncInformers, "sources-time-to-resync-informers", 60*time.Second,
		"Interval to resynchronize all resources in the informers")

	flag.StringVar(&webhooksClientHostname, "webhook-client-hostname", "webhooks.admitik.svc",
		"The hostname used by Kubernetes when calling the webhooks server")
	flag.IntVar(&webhooksClientPort, "webhook-client-port", 10250,
		"The port used by Kubernetes when calling the webhooks server")
	flag.IntVar(&webhooksClientTimeout, "webhook-client-timeout", 10,
		"The time waited by Kubernetes when calling the webhooks server before considering timeout")

	flag.IntVar(&webhooksServerPort, "webhook-server-port", 10250,
		"The port where the webhooks server listens")
	flag.StringVar(&webhooksServerPath, "webhook-server-path", "/admission",
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

	flag.Float64Var(&kubeClientQps, "kube-client-qps", 5.0,
		"The QPS rate of communication between controller and the API Server")
	flag.IntVar(&kubeClientBurst, "kube-client-burst", 10,
		"The burst capacity of communication between the controller and the API Server")

	// Exclusion related flags
	flag.BoolVar(&enableSpecialLabels, "enable-special-labels", false,
		"Enable labels that perform sensitive actions")
	flag.BoolVar(&excludeAdmissionSelfNamespace, "exclude-admission-self-namespace", false,
		"Exclude Admitik resources from admission evaluations")
	flag.StringVar(&excludedAdmissionNamespaces, "excluded-admission-namespaces", "",
		"Comma-separated list of namespaces to be excluded from admission evaluations. Commonly used for 'kube-system'")

	// Ref: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/log/zap@v0.21.0#Options.BindFlags
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Define the context separated as it will be used by our custom controller too.
	// This will synchronize goroutine death when the main controller is killed
	globals.Application.Context = ctrl.SetupSignalHandler()

	// Create and store raw Kubernetes clients from client-go
	// They are used by non kubebuilder processes and controllers
	globals.Application.KubeRawClient,
		globals.Application.KubeRawCoreClient,
		globals.Application.KubeDiscoveryClient,
		err = globals.NewKubernetesClient(&rest.Config{
		QPS:   float32(kubeClientQps),
		Burst: kubeClientBurst,
	})
	if err != nil {
		setupLog.Error(err, "unable to set up kubernetes clients")
		os.Exit(1)
	}

	currentNamespace, err := globals.GetCurrentNamespace()
	if err != nil {
		setupLog.Error(err, "unable to get current namespace")
		os.Exit(1)
	}

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
		WebhookServer:           webhookServer,
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "9ee19594.admitik.dev",
		LeaderElectionNamespace: currentNamespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	///////////////////////////////////
	// Get/Generate certificates needed by admission webhooks server
	// TODO: Extract this entire block to a different function
	var ca, cert, privKey string

	if (webhooksServerCA != "" || webhooksServerCertificate != "" || webhooksServerPrivateKey != "") &&
		(webhooksServerCertsSecretName != "") {
		setupLog.Error(err, "getting certificates from files and from Secret objects are mutually exclusive")
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

	webhookClientConfigValidation, webhookClientConfigMutation := controller.GetSpecificWebhookClientConfigs(
		webhookClientConfig,
		admission.AdmissionServerValidationPath,
		admission.AdmissionServerMutationPath)

	// Create registries managers that will be used by several controllers
	clusterValidationPolicyReg := policyStore.NewPolicyStore[*v1alpha1.ClusterValidationPolicy]()
	clusterMutationPolicyReg := policyStore.NewPolicyStore[*v1alpha1.ClusterMutationPolicy]()
	clusterGenerationPolicyReg := policyStore.NewPolicyStore[*v1alpha1.ClusterGenerationPolicy]()
	sourcesReg := sourcesRegistry.NewSourcesRegistry()
	resourceObserverReg := resourceObserverRegistry.NewResourceObserverRegistry()
	resourceInformerReg := resourceInformerRegistry.NewResourceInformerRegistry()

	// Init internal registries controllers
	// Following controllers manage internal registries for user-facing resources.
	// IMPORTANT: All the replicas are able to process and leader is not chosen for this.
	// TODO: Create a common controller to update all the registries at once (deduplicate code)
	if err = (&clustergenerationpolicy.ClusterGenerationPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		Options: clustergenerationpolicy.ClusterGenerationPolicyControllerOptions{},
		Dependencies: clustergenerationpolicy.ClusterGenerationPolicyControllerDependencies{
			ClusterGenerationPolicyRegistry: clusterGenerationPolicyReg,
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterGenerationPolicy")
		os.Exit(1)
	}

	if err = (&clustermutationpolicy.ClusterMutationPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		Options: clustermutationpolicy.ClusterMutationPolicyControllerOptions{
			CurrentNamespace:              currentNamespace,
			EnableSpecialLabels:           enableSpecialLabels,
			ExcludeAdmissionSelfNamespace: excludeAdmissionSelfNamespace,
			ExcludedAdmissionNamespaces:   excludedAdmissionNamespaces,

			WebhookClientConfig: webhookClientConfigMutation,
			WebhookTimeout:      webhooksClientTimeout,
		},
		Dependencies: clustermutationpolicy.ClusterMutationPolicyControllerDependencies{
			ClusterMutationPolicyRegistry: clusterMutationPolicyReg,
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterMutationPolicy")
		os.Exit(1)
	}

	if err = (&clustervalidationpolicy.ClusterValidationPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		Options: clustervalidationpolicy.ClusterValidationPolicyControllerOptions{
			CurrentNamespace:              currentNamespace,
			EnableSpecialLabels:           enableSpecialLabels,
			ExcludeAdmissionSelfNamespace: excludeAdmissionSelfNamespace,
			ExcludedAdmissionNamespaces:   excludedAdmissionNamespaces,

			WebhookClientConfig: webhookClientConfigValidation,
			WebhookTimeout:      webhooksClientTimeout,
		},
		Dependencies: clustervalidationpolicy.ClusterValidationPolicyControllerDependencies{
			ClusterValidationPolicyRegistry: clusterValidationPolicyReg,
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterValidationPolicy")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	// Init ObservedResourceController.
	// This controller launches watchers for resource types expressed in 'watchedResources' section of some CRs,
	// and executes suitable processors for them.
	// This is used in resources such as ClusterGenerationPolicy, ClusterCleanPolicy, etc.
	observedResourceController := observedresource.ObservedResourceController{
		Client: mgr.GetClient(),
		Options: observedresource.ObservedResourceControllerOptions{
			InformerDurationToResync: sourcesTimeToResyncInformers,
		},
		Dependencies: observedresource.ObservedResourceControllerDependencies{
			Context:                         &globals.Application.Context,
			ClusterGenerationPolicyRegistry: clusterGenerationPolicyReg,
			SourcesRegistry:                 sourcesReg,
			ResourceInformerRegistry:        resourceInformerReg,
			ResourceObserverRegistry:        resourceObserverReg,
		},
	}
	if err = mgr.Add(&observedResourceController); err != nil {
		setupLog.Error(err, "failed adding observed resources controller to manager")
		os.Exit(1)
	}

	// Init SourcesController.
	// This controller is in charge of launching watchers to cache sources expressed in some CRs in background.
	// This way we avoid retrieving them from Kubernetes on each request done by other controllers
	// such as AdmissionServer or BackgroundController.
	// IMPORTANT: All the replicas are able to process and leader is not chosen for this.
	sourcesController := sources.SourcesController{
		Client: mgr.GetClient(),
		Options: sources.SourcesControllerOptions{
			InformerDurationToResync: sourcesTimeToResyncInformers,
		},
		Dependencies: sources.SourcesControllerDependencies{
			Context:                         &globals.Application.Context,
			ClusterGenerationPolicyRegistry: clusterGenerationPolicyReg,
			ClusterMutationPolicyRegistry:   clusterMutationPolicyReg,
			ClusterValidationPolicyRegistry: clusterValidationPolicyReg,
			SourcesRegistry:                 sourcesReg,
		},
	}
	if err = mgr.Add(&sourcesController); err != nil {
		setupLog.Error(err, "failed adding sources controller to manager")
		os.Exit(1)
	}

	// Init AdmissionServer to process incoming validation/mutation events.
	// IMPORTANT: All the replicas are able to process and leader is not chosen for this.
	admissionServer := admission.NewAdmissionServer(
		admission.AdmissionServerOptions{
			//
			ServerAddr: "0.0.0.0",
			ServerPort: webhooksServerPort,
			ServerPath: webhooksServerPath,

			//
			TLSCertificate: webhooksServerCertificate,
			TLSPrivateKey:  webhooksServerPrivateKey,
		},
		admission.AdmissionServerDependencies{
			Context:                         &globals.Application.Context,
			SourcesRegistry:                 sourcesReg,
			ClusterValidationPolicyRegistry: clusterValidationPolicyReg,
			ClusterMutationPolicyRegistry:   clusterMutationPolicyReg,
		})
	if err = mgr.Add(admissionServer); err != nil {
		setupLog.Error(err, "failed adding admission server controller to manager")
		os.Exit(1)
	}

	//
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	//
	setupLog.Info("starting manager")
	if err := mgr.Start(globals.Application.Context); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
