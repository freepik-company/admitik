package controller

import (
	"fmt"
	"net/url"
	"strings"

	admissionregv1 "k8s.io/api/admissionregistration/v1"
)

const (
	//DynamicClusterRoleResourceType = "DynamicClusterRole"
	//DynamicRoleBindingResourceType = "DynamicRoleBinding"

	//
	resourceNotFoundError          = "%s '%s' resource not found. Ignoring since object must be deleted."
	resourceRetrievalError         = "Error getting the %s '%s' from the cluster: %s"
	resourceTargetsDeleteError     = "Failed to delete targets of %s '%s': %s"
	resourceFinalizersUpdateError  = "Failed to update finalizer of %s '%s': %s"
	resourceConditionUpdateError   = "Failed to update the condition on %s '%s': %s"
	resourceSyncTimeRetrievalError = "Can not get synchronization time from the %s '%s': %s"
	syncTargetError                = "Can not sync the target for the %s '%s': %s"

	//
	resourceFinalizer = "admitik.freepik.com/finalizer"
)

// GetWebhookClientConfig return a WebhookClientConfig filled according to if the remote server
// is trully remote or inside Kubernetes
func GetWebhookClientConfig(CABundle []byte, serverHostname string, serverPort int, serverPath string) (wcConfig *admissionregv1.WebhookClientConfig, err error) {

	wcConfig = &admissionregv1.WebhookClientConfig{}

	wcConfig.CABundle = CABundle

	if strings.HasSuffix(serverHostname, ".svc") || strings.HasSuffix(serverHostname, ".svc.cluster.local") {
		tmpWebhooksClientHostname := strings.TrimSuffix(serverHostname, ".svc")
		tmpWebhooksClientHostname = strings.TrimSuffix(tmpWebhooksClientHostname, ".svc.cluster.local")

		hostnameParts := strings.Split(tmpWebhooksClientHostname, ".")
		if len(hostnameParts) != 2 {
			return nil, fmt.Errorf("invalid hostname for internal Kubernetes service. It must match 'x.x.svc' or 'y.y.svc.cluster.local'")
		}

		webhooksClientPortConv := int32(serverPort)
		wcConfig.Service = &admissionregv1.ServiceReference{
			Name:      hostnameParts[0],
			Namespace: hostnameParts[1],
			Port:      &webhooksClientPortConv,
			Path:      &serverPath,
		}
	} else {

		webhooksClientHost := fmt.Sprintf("%s:%d", serverHostname, serverPort)
		webhooksServerUrl := url.URL{
			Scheme: "https",
			Host:   webhooksClientHost,
			Path:   serverPath,
		}

		webhooksServerUrlString := webhooksServerUrl.String()
		wcConfig.URL = &webhooksServerUrlString
	}

	return wcConfig, err
}
