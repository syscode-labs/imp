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

	Context("CRDs", Label("smoke"), func() {
		It("installs all nine CRDs", func() {
			crds := []string{
				"impvms.imp.dev",
				"impvmclasses.imp.dev",
				"impvmtemplates.imp.dev",
				"impnetworks.imp.dev",
				"clusterimpconfigs.imp.dev",
				"clusterimpnodeprofiles.imp.dev",
				"impvmmigrations.imp.dev",
				"impvmsnapshots.imp.dev",
				"impvmrunnerpools.imp.dev",
			}
			for _, crd := range crds {
				cmd := exec.Command("kubectl", "get", "crd", crd)
				_, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CRD %s not found", crd))
			}
		})
	})

	Context("Operator", Label("smoke"), func() {
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

	Context("Webhooks", Label("smoke"), func() {
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

	Context("ImpVM CRUD", Label("smoke"), func() {
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

	Context("RunnerPool demand", Label("smoke"), func() {
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

	Context("Cilium IPAM", Label("nightly-cilium"), func() {
		const (
			poolName    = "e2e-imp-pool"
			networkName = "e2e-cilium-ipam-net"
		)

		AfterEach(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impnetwork", networkName,
				"-n", "default", "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "ciliumpodippool", poolName,
				"--ignore-not-found"))
		})

		It("accepts ImpNetwork with ipam.provider=cilium and existing CiliumPodIPPool", func() {
			By("ensuring CiliumPodIPPool CRD exists")
			if _, err := utils.Run(exec.Command("kubectl", "get", "crd", "ciliumpodippools.cilium.io")); err != nil {
				Skip("CiliumPodIPPool CRD not installed in this cluster")
			}

			By("creating CiliumPodIPPool")
			poolManifest := fmt.Sprintf(`
apiVersion: cilium.io/v2alpha1
kind: CiliumPodIPPool
metadata:
  name: %s
spec:
  ipv4:
    cidrs:
      - 10.123.0.0/24
    maskSize: 30
`, poolName)
			applyPool := exec.Command("kubectl", "apply", "-f", "-")
			applyPool.Stdin = strings.NewReader(poolManifest)
			_, err := utils.Run(applyPool)
			Expect(err).NotTo(HaveOccurred())

			By("creating ImpNetwork that delegates IPAM to Cilium pool")
			netManifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpNetwork
metadata:
  name: %s
  namespace: default
spec:
  subnet: 10.44.0.0/24
  ipam:
    provider: cilium
    cilium:
      poolRef: %s
`, networkName, poolName)
			applyNet := exec.Command("kubectl", "apply", "-f", "-")
			applyNet.Stdin = strings.NewReader(netManifest)
			_, err = utils.Run(applyNet)
			Expect(err).NotTo(HaveOccurred())

			By("verifying poolRef is persisted")
			Eventually(func(g Gomega) {
				getCmd := exec.Command("kubectl", "get", "impnetwork", networkName, "-n", "default",
					"-o", "jsonpath={.spec.ipam.cilium.poolRef}")
				out, getErr := utils.Run(getCmd)
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).To(Equal(poolName))
			}).Should(Succeed())
		})
	})

	Context("Metrics", Label("smoke"), func() {
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

	Context("Scheduling filter", Label("smoke"), Ordered, func() {
		var nodeName string

		BeforeAll(func() {
			By("getting Kind node name")
			out, err := utils.Run(exec.Command("kubectl", "get", "nodes",
				"-o", "jsonpath={.items[0].metadata.name}"))
			Expect(err).NotTo(HaveOccurred())
			nodeName = strings.TrimSpace(out)
			Expect(nodeName).NotTo(BeEmpty())

			By("removing control-plane taint so scheduler can use the node")
			_, _ = utils.Run(exec.Command("kubectl", "taint", "nodes", nodeName,
				"node-role.kubernetes.io/control-plane:NoSchedule-"))

			By("labeling node imp/enabled=true")
			_, err = utils.Run(exec.Command("kubectl", "label", "node", nodeName,
				"imp/enabled=true", "--overwrite"))
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			By("removing imp/enabled label")
			_, _ = utils.Run(exec.Command("kubectl", "label", "node", nodeName,
				"imp/enabled-"))
		})

		AfterEach(func() {
			for _, name := range []string{"e2e-sched-cordon", "e2e-sched-notoleration", "e2e-sched-toleration"} {
				_, _ = utils.Run(exec.Command("kubectl", "delete", "impvm", name,
					"-n", "default", "--ignore-not-found"))
			}
			_, _ = utils.Run(exec.Command("kubectl", "uncordon", nodeName))
			_, _ = utils.Run(exec.Command("kubectl", "taint", "nodes", nodeName,
				"e2e-test=blocked:NoSchedule-"))
		})

		It("keeps VM Pending on cordoned node, schedules after uncordon", func() {
			By("cordoning the node")
			_, err := utils.Run(exec.Command("kubectl", "cordon", nodeName))
			Expect(err).NotTo(HaveOccurred())

			By("creating ImpVM e2e-sched-cordon")
			manifest := `
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: e2e-sched-cordon
  namespace: default
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
  lifecycle: ephemeral
`
			applyCmd := exec.Command("kubectl", "apply", "-f", "-")
			applyCmd.Stdin = strings.NewReader(manifest)
			_, err = utils.Run(applyCmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying VM stays Pending (no nodeName) while node is cordoned")
			Consistently(func(g Gomega) {
				getCmd := exec.Command("kubectl", "get", "impvm", "e2e-sched-cordon",
					"-n", "default", "-o", "jsonpath={.spec.nodeName}")
				out, getErr := utils.Run(getCmd)
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).To(BeEmpty())
			}, 20*time.Second, time.Second).Should(Succeed())

			By("uncordoning the node")
			_, err = utils.Run(exec.Command("kubectl", "uncordon", nodeName))
			Expect(err).NotTo(HaveOccurred())

			By("verifying VM gets scheduled (nodeName set)")
			Eventually(func(g Gomega) {
				getCmd := exec.Command("kubectl", "get", "impvm", "e2e-sched-cordon",
					"-n", "default", "-o", "jsonpath={.spec.nodeName}")
				out, getErr := utils.Run(getCmd)
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
			}, 5*time.Minute, 2*time.Second).Should(Succeed())
		})

		It("keeps VM Pending with unmatched taint, schedules VM with matching toleration", func() {
			By("adding custom taint e2e-test=blocked:NoSchedule to node")
			_, err := utils.Run(exec.Command("kubectl", "taint", "nodes", nodeName,
				"e2e-test=blocked:NoSchedule"))
			Expect(err).NotTo(HaveOccurred())

			By("creating ImpVM without toleration")
			noTolManifest := `
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: e2e-sched-notoleration
  namespace: default
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
  lifecycle: ephemeral
`
			applyCmd := exec.Command("kubectl", "apply", "-f", "-")
			applyCmd.Stdin = strings.NewReader(noTolManifest)
			_, err = utils.Run(applyCmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying VM without toleration stays Pending")
			Consistently(func(g Gomega) {
				getCmd := exec.Command("kubectl", "get", "impvm", "e2e-sched-notoleration",
					"-n", "default", "-o", "jsonpath={.spec.nodeName}")
				out, getErr := utils.Run(getCmd)
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).To(BeEmpty())
			}, 20*time.Second, time.Second).Should(Succeed())

			By("deleting VM without toleration")
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impvm", "e2e-sched-notoleration",
				"-n", "default", "--ignore-not-found"))

			By("creating ImpVM with matching toleration")
			tolManifest := `
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: e2e-sched-toleration
  namespace: default
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
  lifecycle: ephemeral
  tolerations:
    - key: e2e-test
      operator: Equal
      value: blocked
      effect: NoSchedule
`
			applyCmd2 := exec.Command("kubectl", "apply", "-f", "-")
			applyCmd2.Stdin = strings.NewReader(tolManifest)
			_, err = utils.Run(applyCmd2)
			Expect(err).NotTo(HaveOccurred())

			By("verifying VM with matching toleration gets scheduled")
			Eventually(func(g Gomega) {
				getCmd := exec.Command("kubectl", "get", "impvm", "e2e-sched-toleration",
					"-n", "default", "-o", "jsonpath={.spec.nodeName}")
				out, getErr := utils.Run(getCmd)
				g.Expect(getErr).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
			}).Should(Succeed())
		})
	})
})
