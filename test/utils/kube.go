package utils

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// Kubectl runs kubectl against the cluster selected by the KUBECONFIG of the test process
func Kubectl(args ...string) (string, error) {
	output, err := Run(exec.Command("kubectl", args...))
	return strings.TrimSpace(string(output)), err
}

// KubectlApply applies a manifest given as a string
func KubectlApply(manifest string) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	_, err := Run(cmd)
	return err
}

// ObjectExists reports whether an object exists. Any error other than NotFound is returned,
// so a transient failure is never mistaken for the object being absent
func ObjectExists(resource, namespace, name string) (bool, error) {
	output, err := Kubectl("get", resource, name, "-n", namespace, "-o", "name")
	if err == nil {
		return true, nil
	}
	if strings.Contains(output, "NotFound") || strings.Contains(err.Error(), "NotFound") {
		return false, nil
	}
	return false, err
}

// EnsureLocalKindCluster refuses to continue unless the selected context is a kind cluster
// whose API server listens on a loopback address
func EnsureLocalKindCluster() error {
	kubeContext, err := Kubectl("config", "current-context")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(kubeContext, "kind-") {
		return fmt.Errorf("refusing to run e2e tests: context %q is not a local kind cluster", kubeContext)
	}

	server, err := Kubectl("config", "view", "--minify", "-o", "jsonpath={.clusters[0].cluster.server}")
	if err != nil {
		return err
	}
	parsed, err := url.Parse(server)
	if err != nil {
		return fmt.Errorf("parsing API server address %q: %w", server, err)
	}
	switch parsed.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return nil
	}

	return fmt.Errorf("refusing to run e2e tests: context %q points to non-local server %q", kubeContext, server)
}
