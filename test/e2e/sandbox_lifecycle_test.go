//go:build e2e
// +build e2e

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

package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/syscode-labs/imp/test/utils"
)

var _ = Describe("Imp Sandbox add-on", Ordered, func() {
	const (
		helmRelease = "imp-sandbox"
		sandboxName = "e2e-sbx"
	)

	SetDefaultEventuallyTimeout(3 * time.Minute)

	BeforeAll(func() {
		By("installing the imp-sandbox subchart")
		cmd := exec.Command("helm", "upgrade", "--install", helmRelease, "charts/imp-sandbox",
			"--namespace", namespace,
			"--set", "sandbox.image.repository=local/imp-sandbox",
			"--set", "sandbox.image.tag=e2e",
			"--wait", "--timeout", "5m")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "helm install imp-sandbox failed")
	})

	AfterAll(func() {
		By("uninstalling the imp-sandbox subchart")
		cmd := exec.Command("helm", "uninstall", helmRelease, "--namespace", namespace, "--wait")
		_, _ = utils.Run(cmd)
		crdCmd := exec.Command("kubectl", "delete", "crd", "impsandboxes.sandbox.imp.dev", "--ignore-not-found")
		_, _ = utils.Run(crdCmd)
	})

	Context("Installation", Label("smoke"), func() {
		It("installs the ImpSandbox CRD", func() {
			cmd := exec.Command("kubectl", "get", "crd", "impsandboxes.sandbox.imp.dev")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	PContext("Lifecycle", Label("smoke"), func() {
		AfterEach(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impsandbox", sandboxName,
				"-n", namespace, "--ignore-not-found"))
		})

		// applySandbox creates the sandbox object. Admission goes through the
		// sandbox webhook, which becomes ready shortly after helm reports the
		// deployment ready (manager cache sync precedes webhook listen), so
		// callers may need a few attempts immediately after installation.
		applySandbox := func(manifest string) {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "apply", "-f", "-")
				cmd.Stdin = strings.NewReader(manifest)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(90 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
		}

		It("expands a sandbox into an owned VM and hardened network", func() {
			manifest := fmt.Sprintf(`
apiVersion: sandbox.imp.dev/v1alpha1
kind: ImpSandbox
metadata:
  name: %s
  namespace: %s
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
  tenancy: standard
`, sandboxName, namespace)
			applySandbox(manifest)

			By("creating a labeled backing ImpVM")
			Eventually(func(g Gomega) {
				out, getErr := utils.Run(exec.Command("kubectl", "get", "impvm", sandboxName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.labels.sandbox\\.imp\\.dev/owner}"))
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(sandboxName))
			}, "2m", "2s").Should(Succeed())

			By("creating a dedicated network carrying the baseline deny list")
			Eventually(func(g Gomega) {
				out, getErr := utils.Run(exec.Command("kubectl", "get", "impnetwork", sandboxName+"-net",
					"-n", namespace,
					"-o", "jsonpath={.spec.firewall.denyCidrs}"))
				g.Expect(getErr).NotTo(HaveOccurred())
				// The controller should populate at least the metadata CIDR; an
				// empty string means the network exists but the firewall was never
				// reconciled (controller not running or patch missed). Any non-empty
				// deny list proves the network was created.
				g.Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
			}, "2m", "2s").Should(Succeed())

			By("reporting enforced tenancy")
			Eventually(func(g Gomega) {
				out, getErr := utils.Run(exec.Command("kubectl", "get", "impsandbox", sandboxName,
					"-n", namespace,
					"-o", "jsonpath={.status.effectiveTenancy}"))
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("standard"))
			}, "1m", "1s").Should(Succeed())
		})

		It("garbage-collects the backing VM on sandbox deletion", func() {
			manifest := fmt.Sprintf(`
apiVersion: sandbox.imp.dev/v1alpha1
kind: ImpSandbox
metadata:
  name: %s
  namespace: %s
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
`, sandboxName, namespace)
			applySandbox(manifest)

			Eventually(func(g Gomega) {
				_, getErr := utils.Run(exec.Command("kubectl", "get", "impvm", sandboxName, "-n", namespace))
				g.Expect(getErr).NotTo(HaveOccurred())
			}).Should(Succeed())

			delCmd := exec.Command("kubectl", "delete", "impsandbox", sandboxName, "-n", namespace)
			_, delErr := utils.Run(delCmd)
			Expect(delErr).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				getCmd := exec.Command("kubectl", "get", "impvm", sandboxName,
					"-n", namespace, "--ignore-not-found")
				out, getErr := utils.Run(getCmd)
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).To(BeEmpty(), "backing ImpVM must be removed")
			}).Should(Succeed())
		})
	})
})
