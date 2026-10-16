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
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/freepik-company/admitik/test/utils"
)

const (
	cleanupNamespace = "e2e-cleanup"

	generationTimeout = 2 * time.Minute
	pollInterval      = 4 * time.Second
	stableFor         = 20 * time.Second

	onLabelCondition = "has(object.metadata.labels) && 'e2e/gen' in object.metadata.labels && " +
		"object.metadata.labels['e2e/gen'] == 'true'"
	errAnnotationCondition = "object.metadata.name == 'wl-err' && object.metadata.annotations['e2e/mode'] == 'on'"
)

var touches atomic.Int64

func generationPolicy(name, suffix, condition string, deleteOnConditionsUnmet bool) string {
	return fmt.Sprintf(`apiVersion: admitik.dev/v1alpha1
kind: ClusterGenerationPolicy
metadata:
  name: %[1]s
spec:
  overwriteExisting: true
  deleteOnConditionsUnmet: %[4]t
  watchedResources:
    - group: apps
      version: v1
      resource: deployments
      namespace: %[5]s
  sources: []
  conditions:
    - name: trigger
      engine: cel
      key: |
        %[3]s
      value: "true"
  object:
    clone: {}
    definition:
      engine: plain+cel
      template: |
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: "{{cel: object.metadata.name }}-%[2]s"
          namespace: "{{cel: object.metadata.namespace }}"
        data:
          generated-by: %[1]s
`, name, suffix, condition, deleteOnConditionsUnmet, cleanupNamespace)
}

func workload(name string, labels, annotations map[string]string) string {
	render := func(values map[string]string) string {
		var lines []string
		for key, value := range values {
			lines = append(lines, fmt.Sprintf("    %s: %q", key, value))
		}
		return strings.Join(lines, "\n")
	}

	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %[1]s
  namespace: %[2]s
  labels:
%[3]s
  annotations:
%[4]s
spec:
  replicas: 0
  selector:
    matchLabels:
      app: %[1]s
  template:
    metadata:
      labels:
        app: %[1]s
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
`, name, cleanupNamespace, render(labels), render(annotations))
}

// touch forces a new event for a workload, as events fired right after an informer starts can be missed
func touch(deployment string) {
	_, _ = utils.Kubectl("label", "deployment", deployment, "-n", cleanupNamespace,
		fmt.Sprintf("e2e/touch=%d", touches.Add(1)), "--overwrite")
}

func configMapExists(name string) (bool, error) {
	return utils.ObjectExists("configmap", cleanupNamespace, name)
}

func eventuallyGenerated(parent, configMap string) {
	EventuallyWithOffset(1, func(g Gomega) {
		touch(parent)
		exists, err := configMapExists(configMap)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(exists).To(BeTrue(), "configmap %s not generated yet", configMap)
	}, generationTimeout, pollInterval).Should(Succeed())
}

func eventuallyDeleted(configMap string) {
	EventuallyWithOffset(1, func(g Gomega) {
		exists, err := configMapExists(configMap)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(exists).To(BeFalse(), "configmap %s still exists", configMap)
	}, generationTimeout, pollInterval).Should(Succeed())
}

func consistentlyPresent(configMap string) {
	ConsistentlyWithOffset(1, func(g Gomega) {
		exists, err := configMapExists(configMap)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(exists).To(BeTrue(), "configmap %s was deleted", configMap)
	}, stableFor, 2*time.Second).Should(Succeed())
}

func consistentlyAbsent(configMap string) {
	ConsistentlyWithOffset(1, func(g Gomega) {
		exists, err := configMapExists(configMap)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(exists).To(BeFalse(), "configmap %s was regenerated", configMap)
	}, stableFor, 2*time.Second).Should(Succeed())
}

func removeLabel(deployment, label string) {
	_, err := utils.Kubectl("label", "deployment", deployment, "-n", cleanupNamespace, label+"-")
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
}

func removeAnnotation(deployment, annotation string) {
	_, err := utils.Kubectl("annotate", "deployment", deployment, "-n", cleanupNamespace, annotation+"-")
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
}

var _ = Describe("ClusterGenerationPolicy deleteOnConditionsUnmet", Ordered, ContinueOnFailure, func() {
	on := map[string]string{"e2e/gen": "true"}

	BeforeAll(func() {
		_, _ = utils.Kubectl("create", "ns", cleanupNamespace)

		By("creating an object that was not generated by admitik but uses a generated name")
		Expect(utils.KubectlApply(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: wl-existing-on
  namespace: %s
data:
  owner: human
`, cleanupNamespace))).To(Succeed())

		By("creating the policies: with the flag, without it, and with a condition that can fail")
		Expect(utils.KubectlApply(generationPolicy("e2e-cleanup-on", "on", onLabelCondition, true))).To(Succeed())
		Expect(utils.KubectlApply(generationPolicy("e2e-cleanup-off", "off", onLabelCondition, false))).To(Succeed())
		Expect(utils.KubectlApply(generationPolicy("e2e-cleanup-err", "err", errAnnotationCondition, true))).To(Succeed())

		By("creating the workloads")
		for _, name := range []string{"wl-a", "wl-b", "wl-c", "wl-d", "wl-existing"} {
			Expect(utils.KubectlApply(workload(name, on, map[string]string{"e2e/note": "x"}))).To(Succeed())
		}
		Expect(utils.KubectlApply(workload("wl-err", map[string]string{"e2e/keep": "true"},
			map[string]string{"e2e/mode": "on"}))).To(Succeed())

		By("waiting for every object to be generated")
		for _, name := range []string{"wl-a", "wl-b", "wl-c", "wl-d"} {
			eventuallyGenerated(name, name+"-on")
			eventuallyGenerated(name, name+"-off")
		}
		eventuallyGenerated("wl-existing", "wl-existing-off")
		eventuallyGenerated("wl-err", "wl-err-err")
	})

	AfterAll(func() {
		_, _ = utils.Kubectl("delete", "clustergenerationpolicy", "e2e-cleanup-on", "e2e-cleanup-off",
			"e2e-cleanup-err", "--ignore-not-found", "--timeout=60s")
		_, _ = utils.Kubectl("delete", "ns", cleanupNamespace, "--ignore-not-found", "--timeout=120s")
	})

	It("marks only the objects it created", func() {
		labels, err := utils.Kubectl("get", "configmap", "wl-a-on", "-n", cleanupNamespace,
			"-o", "jsonpath={.metadata.labels}")
		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(ContainSubstring("admitik.dev/policy"))

		labels, err = utils.Kubectl("get", "configmap", "wl-existing-on", "-n", cleanupNamespace,
			"-o", "jsonpath={.metadata.labels}")
		Expect(err).NotTo(HaveOccurred())
		Expect(labels).NotTo(ContainSubstring("admitik.dev/policy"), "a pre-existing object must not be adopted")
	})

	It("deletes only the object of the workload that stopped matching", func() {
		removeLabel("wl-a", "e2e/gen")

		eventuallyDeleted("wl-a-on")
		consistentlyPresent("wl-b-on")
		consistentlyPresent("wl-a-off")
	})

	It("does not delete an object it did not create", func() {
		removeLabel("wl-c", "e2e/gen")
		removeLabel("wl-existing", "e2e/gen")

		By("the paired owned object proves the events were processed")
		eventuallyDeleted("wl-c-on")
		consistentlyPresent("wl-existing-on")
	})

	It("does not delete anything when evaluating the condition fails", func() {
		removeLabel("wl-d", "e2e/gen")
		removeAnnotation("wl-err", "e2e/mode")

		By("the paired owned object proves the events were processed")
		eventuallyDeleted("wl-d-on")
		consistentlyPresent("wl-err-err")
	})

	It("deletes the generated object when the workload is deleted and does not regenerate it", func() {
		_, err := utils.Kubectl("delete", "deployment", "wl-b", "-n", cleanupNamespace)
		Expect(err).NotTo(HaveOccurred())

		eventuallyDeleted("wl-b-on")
		consistentlyAbsent("wl-b-on")
		consistentlyPresent("wl-b-off")
	})
})
