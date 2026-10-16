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

package e2e

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/freepik-company/admitik/test/utils"
)

const (
	namespace    = "admitik-system"
	projectImage = "example.com/admitik:v0.0.1"
)

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting admitik suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("checking the selected cluster is a local kind cluster")
	Expect(utils.EnsureLocalKindCluster()).To(Succeed())

	By("building the manager(Operator) image")
	_, err := utils.Run(exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectImage)))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	By("loading the the manager(Operator) image on Kind")
	ExpectWithOffset(1, utils.LoadImageToKindClusterWithName(projectImage)).To(Succeed())

	By("creating manager namespace")
	_, _ = utils.Run(exec.Command("kubectl", "create", "ns", namespace))

	By("installing CRDs")
	_, err = utils.Run(exec.Command("make", "install"))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	By("deploying the controller-manager")
	_, err = utils.Run(exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage)))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	By("waiting for the controller-manager to be running")
	EventuallyWithOffset(1, verifyControllerUp, 2*time.Minute, time.Second).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("undeploying the controller-manager")
	_, _ = utils.Run(exec.Command("make", "undeploy", "ignore-not-found=true"))

	By("uninstalling CRDs")
	_, _ = utils.Run(exec.Command("make", "uninstall", "ignore-not-found=true"))

	By("removing manager namespace")
	_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found"))
})

// verifyControllerUp succeeds once exactly one controller-manager pod is Running
func verifyControllerUp() error {
	podOutput, err := utils.Kubectl("get", "pods", "-l", "control-plane=controller-manager",
		"-o", "go-template={{ range .items }}"+
			"{{ if not .metadata.deletionTimestamp }}"+
			"{{ .metadata.name }}"+
			"{{ \"\\n\" }}{{ end }}{{ end }}",
		"-n", namespace)
	if err != nil {
		return err
	}

	podNames := utils.GetNonEmptyLines(podOutput)
	if len(podNames) != 1 {
		return fmt.Errorf("expect 1 controller pods running, but got %d", len(podNames))
	}

	status, err := utils.Kubectl("get", "pods", podNames[0], "-o", "jsonpath={.status.phase}", "-n", namespace)
	if err != nil {
		return err
	}
	if status != "Running" {
		return fmt.Errorf("controller pod %s in %s status", podNames[0], status)
	}

	return nil
}
