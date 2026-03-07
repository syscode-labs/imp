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
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/syscode-labs/imp/test/utils"
)

var _ = Describe("Imp operator", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("CRDs", func() {
		It("installs all six CRDs", func() {
			crds := []string{
				"impvms.imp.dev",
				"impvmclasses.imp.dev",
				"impvmtemplates.imp.dev",
				"impnetworks.imp.dev",
				"clusterimpconfigs.imp.dev",
				"clusterimpnodeprofiles.imp.dev",
			}
			for _, crd := range crds {
				cmd := exec.Command("kubectl", "get", "crd", crd)
				_, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CRD %s not found", crd))
			}
		})
	})

	Context("Operator", func() {
		It("starts and passes health checks", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/instance=%s,app.kubernetes.io/component=operator", helmRelease),
					"-n", namespace,
					"-o", "jsonpath={.items[0].status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Running"))
			}).Should(Succeed())
		})
	})

	Context("Webhooks", func() {
		It("rejects an ImpVM with missing classRef", func() {
			manifest := `
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: invalid-vm
  namespace: default
spec:
  image: ghcr.io/syscode-labs/test:latest
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "expected webhook to reject ImpVM with missing classRef")
		})
	})

	Context("ImpVM CRUD", func() {
		const vmName = "e2e-smoke"
		AfterEach(func() {
			cleanCmd := exec.Command("kubectl", "delete", "impvm", vmName, "-n", "default", "--ignore-not-found")
			_, _ = utils.Run(cleanCmd)
		})

		It("creates and lists an ImpVM", func() {
			manifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: %s
  namespace: default
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
  lifecycle: ephemeral
`, vmName)
			applyCmd := exec.Command("kubectl", "apply", "-f", "-")
			applyCmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(applyCmd)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				getCmd := exec.Command("kubectl", "get", "impvm", vmName, "-n", "default",
					"-o", "jsonpath={.metadata.name}")
				out, getErr := utils.Run(getCmd)
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(vmName))
			}).Should(Succeed())
		})
	})

	Context("RunnerPool demand", func() {
		const (
			templateName = "e2e-runner-template"
			poolName     = "e2e-runner-pool"
		)

		AfterEach(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impvmrunnerpool", poolName, "-n", "default", "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impvmtemplate", templateName, "-n", "default", "--ignore-not-found"))
		})

		It("scales from webhook demand when minIdle is zero", func() {
			templateManifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpVMTemplate
metadata:
  name: %s
  namespace: default
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
`, templateName)
			tplApply := exec.Command("kubectl", "apply", "-f", "-")
			tplApply.Stdin = strings.NewReader(templateManifest)
			_, err := utils.Run(tplApply)
			Expect(err).NotTo(HaveOccurred())

			poolManifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpVMRunnerPool
metadata:
  name: %s
  namespace: default
  annotations:
    imp.dev/runner-demand: "2"
spec:
  templateName: %s
  platform:
    type: github-actions
    credentialsSecret: ignored-when-webhook-only
  scaling:
    minIdle: 0
    maxConcurrent: 5
  jobDetection:
    webhook:
      enabled: true
`, poolName, templateName)
			poolApply := exec.Command("kubectl", "apply", "-f", "-")
			poolApply.Stdin = strings.NewReader(poolManifest)
			_, err = utils.Run(poolApply)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				getCmd := exec.Command("kubectl", "get", "impvms", "-n", "default",
					"-l", "imp.dev/runner-pool="+poolName,
					"-o", "jsonpath={.items[*].metadata.name}")
				out, getErr := utils.Run(getCmd)
				g.Expect(getErr).NotTo(HaveOccurred())
				trimmed := strings.TrimSpace(out)
				if trimmed == "" {
					g.Expect(0).To(Equal(2))
					return
				}
				g.Expect(len(strings.Fields(trimmed))).To(Equal(2))
			}).Should(Succeed())
		})
	})

	Context("Metrics", func() {
		It("operator /metrics endpoint responds 200", func() {
			pf := exec.Command("kubectl", "port-forward",
				fmt.Sprintf("svc/%s-operator", helmRelease),
				"18080:8080", "-n", namespace)
			Expect(pf.Start()).To(Succeed())
			DeferCleanup(func() {
				if pf.Process != nil {
					_ = pf.Process.Kill()
				}
			})

			// Eventually handles connection-refused while port-forward is starting up.
			Eventually(func(g Gomega) {
				resp, err := http.Get("http://localhost:18080/metrics") //nolint:noctx
				g.Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close() //nolint:errcheck
				g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}).Should(Succeed())
		})
	})
})
