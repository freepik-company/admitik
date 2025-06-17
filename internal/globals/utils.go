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
	"errors"
	"os"
	"strings"

	//
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NewKubernetesClient return a new Kubernetes Dynamic client from client-go SDK
func NewKubernetesClient(options *rest.Config) (client *dynamic.DynamicClient, coreClient *kubernetes.Clientset, discoveryClient *discovery.DiscoveryClient, err error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return client, coreClient, discoveryClient, err
	}

	config.QPS = options.QPS
	config.Burst = options.Burst

	// Create the clients to do requests to our friend: Kubernetes
	// Discovery client
	discoveryClient, err = discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return client, coreClient, discoveryClient, err
	}

	// Dynamic client
	client, err = dynamic.NewForConfig(config)
	if err != nil {
		return client, coreClient, discoveryClient, err
	}

	// Core client
	coreClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return client, coreClient, discoveryClient, err
	}

	return client, coreClient, discoveryClient, err
}

// GetCurrentNamespace return namespace where Admitik is running using different strategies
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

type ObjectGVK struct {
	Group   string
	Version string
	Kind    string
}

// GetObjectGVK extracts GVK from an object
func GetObjectGVK(object *map[string]any) (objectData ObjectGVK, err error) {

	objectData = ObjectGVK{}

	value, ok := (*object)["apiVersion"]
	if !ok {
		err = errors.New("apiVersion not found")
		return
	}

	apiVersion := value.(string)

	apiVersionParts := strings.Split(apiVersion, "/")
	if len(apiVersionParts) == 2 {
		objectData.Group = apiVersionParts[0]
		objectData.Version = apiVersionParts[1]
	} else {
		objectData.Version = apiVersionParts[0]
	}

	//
	value, ok = (*object)["kind"]
	if !ok {
		err = errors.New("kind not found")
		return
	}
	objectData.Kind = value.(string)

	return objectData, nil
}

type ObjectBasicData struct {
	Group     string
	Version   string
	Kind      string
	Name      string
	Namespace string
}

// GetObjectBasicData extracts basic data (apiVersion, kind, namespace and name) from the object
func GetObjectBasicData(object *map[string]any) (objectData ObjectBasicData, err error) {

	metadata, ok := (*object)["metadata"].(map[string]any)
	if !ok {
		err = errors.New("field not found or not in expected format: metadata")
		return
	}

	gvk, err := GetObjectGVK(object)
	if err != nil {
		err = errors.New("field not found or not in expected format: group, version, kind")
		return
	}

	objectData = ObjectBasicData{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}

	if value, ok := metadata["name"]; ok {
		objectData.Name = value.(string)
	}

	if value, ok := metadata["namespace"]; ok {
		objectData.Namespace = value.(string)
	}

	return objectData, nil
}
