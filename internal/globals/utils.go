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

package globals

import (

	//
	"os"

	//
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NewKubernetesClient return a new Kubernetes Dynamic client from client-go SDK
func NewKubernetesClient() (client *dynamic.DynamicClient, coreClient *kubernetes.Clientset, err error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return client, coreClient, err
	}

	// Create the clients to do requests to our friend: Kubernetes
	// Dynamic client
	client, err = dynamic.NewForConfig(config)
	if err != nil {
		return client, coreClient, err
	}

	// Core client
	coreClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return client, coreClient, err
	}

	return client, coreClient, err
}

// TODO
func GetCurrentNamespace() (string, error) {
	//
	if _, err := rest.InClusterConfig(); err == nil {
		data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	// From outside, obtain the namespace from kubeconfig file
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		return "", err
	}

	return namespace, nil
}
