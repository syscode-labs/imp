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
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/syscode-labs/imp/test/utils"
)

// Wake-on-traffic datapath e2e.
//
// This validates the one thing the host-only unit + envtest coverage cannot: that
// the agent's AF_PACKET hook actually observes the first frame destined to a
// TAP-less (suspended) ScaleToZero VM on imp's VXLAN overlay, and that a real
// Firecracker suspend/resume round-trips.
//
// UNVALIDATED: this spec has never run against a live KVM node — no CI job wires
// IMP_E2E_REAL_AGENT=true onto a KVM runner yet. Skips itself unless that env var
// is set, so it stays inert everywhere else. Traffic source: a second always-on
// VM on the same ImpNetwork execs `ping` into the suspended VM's overlay IP via
// the agent's vsock guest-exec API (internal/agent/api/exec.go) — guest agent
// injection defaults to enabled, so no extra fixture wiring needed for that part.
var _ = Describe("Imp ScaleToZero datapath", Label("datapath"), func() {
	const (
		networkName = "e2e-sz-datapath-net"
		className   = "e2e-sz-datapath"
		pingerName  = "e2e-sz-datapath-pinger"
		targetName  = "e2e-sz-datapath-target"
	)

	It("wakes a suspended ScaleToZero VM when a frame arrives for its overlay IP", func() {
		if os.Getenv("IMP_E2E_REAL_AGENT") != "true" {
			Skip("requires a KVM node + real Firecracker agent (IMP_E2E_REAL_AGENT=true); see the e2e runner runbook")
		}

		DeferCleanup(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impvm", targetName, "-n", "default", "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impvm", pingerName, "-n", "default", "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impnetwork", networkName, "-n", "default", "--ignore-not-found"))
			_, _ = utils.Run(exec.Command("kubectl", "delete", "impvmclass", className, "--ignore-not-found"))
		})

		By("getting the Kind node name")
		nodeOut, err := utils.Run(exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[0].metadata.name}"))
		Expect(err).NotTo(HaveOccurred())
		nodeName := strings.TrimSpace(nodeOut)
		Expect(nodeName).NotTo(BeEmpty())

		By("removing control-plane taint and labeling the node imp/enabled=true")
		_, _ = utils.Run(exec.Command("kubectl", "taint", "nodes", nodeName,
			"node-role.kubernetes.io/control-plane:NoSchedule-"))
		_, err = utils.Run(exec.Command("kubectl", "label", "node", nodeName, "imp/enabled=true", "--overwrite"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_, _ = utils.Run(exec.Command("kubectl", "label", "node", nodeName, "imp/enabled-"))
		})

		By("creating the ImpVMClass")
		classManifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpVMClass
metadata:
  name: %s
spec:
  vcpu: 1
  memoryMiB: 256
  diskGiB: 1
`, className)
		applyClass := exec.Command("kubectl", "apply", "-f", "-")
		applyClass.Stdin = strings.NewReader(classManifest)
		_, err = utils.Run(applyClass)
		Expect(err).NotTo(HaveOccurred())

		By("creating the ImpNetwork")
		netManifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpNetwork
metadata:
  name: %s
  namespace: default
spec:
  subnet: 10.45.0.0/24
`, networkName)
		applyNet := exec.Command("kubectl", "apply", "-f", "-")
		applyNet.Stdin = strings.NewReader(netManifest)
		_, err = utils.Run(applyNet)
		Expect(err).NotTo(HaveOccurred())

		By("creating the always-on pinger VM")
		pingerManifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: %s
  namespace: default
spec:
  classRef:
    name: %s
  networkRef:
    name: %s
  image: ghcr.io/syscode-labs/test:latest
`, pingerName, className, networkName)
		applyPinger := exec.Command("kubectl", "apply", "-f", "-")
		applyPinger.Stdin = strings.NewReader(pingerManifest)
		_, err = utils.Run(applyPinger)
		Expect(err).NotTo(HaveOccurred())

		By("creating the ScaleToZero target VM with a short idleTimeout")
		targetManifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: %s
  namespace: default
spec:
  classRef:
    name: %s
  networkRef:
    name: %s
  image: ghcr.io/syscode-labs/test:latest
  desiredState: ScaleToZero
  idleTimeout: 15s
`, targetName, className, networkName)
		applyTarget := exec.Command("kubectl", "apply", "-f", "-")
		applyTarget.Stdin = strings.NewReader(targetManifest)
		_, err = utils.Run(applyTarget)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for both VMs to reach Running")
		Eventually(func(g Gomega) {
			phase, _ := vmPhaseAndIP(g, pingerName)
			g.Expect(phase).To(Equal("Running"))
		}, "5m", "5s").Should(Succeed())

		var targetIP string
		Eventually(func(g Gomega) {
			phase, ip := vmPhaseAndIP(g, targetName)
			g.Expect(phase).To(Equal("Running"))
			g.Expect(ip).NotTo(BeEmpty())
			targetIP = ip
		}, "5m", "5s").Should(Succeed())

		By("waiting for the target to auto-suspend after going idle")
		Eventually(func(g Gomega) {
			phase, _ := vmPhaseAndIP(g, targetName)
			g.Expect(phase).To(Equal("Suspended"))
		}, "2m", "5s").Should(Succeed())

		By("finding the agent pod colocated with the pinger VM")
		pingerNode := vmNodeName(pingerName)
		Expect(pingerNode).NotTo(BeEmpty())
		agentPod := agentPodOnNode(pingerNode)
		Expect(agentPod).NotTo(BeEmpty())

		By("port-forwarding to the agent's guest-exec API")
		pf := exec.Command("kubectl", "port-forward", "pod/"+agentPod, "19091:9091", "-n", namespace)
		Expect(pf.Start()).To(Succeed())
		DeferCleanup(func() {
			if pf.Process != nil {
				_ = pf.Process.Kill()
			}
		})
		Eventually(func(g Gomega) {
			conn, dialErr := net.DialTimeout("tcp", "localhost:19091", 2*time.Second)
			g.Expect(dialErr).NotTo(HaveOccurred())
			_ = conn.Close()
		}, "30s", "1s").Should(Succeed())

		By("pinging the suspended VM's overlay IP from the pinger VM's guest agent")
		body, err := json.Marshal(map[string][]string{"command": {"ping", "-c", "3", "-W", "2", targetIP}})
		Expect(err).NotTo(HaveOccurred())
		resp, err := http.Post( //nolint:noctx
			fmt.Sprintf("http://localhost:19091/v1/exec/default/%s", pingerName),
			"application/json", strings.NewReader(string(body)))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close() //nolint:errcheck
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		By("asserting the target wakes: Suspended -> Resuming -> Running")
		Eventually(func(g Gomega) {
			phase, _ := vmPhaseAndIP(g, targetName)
			g.Expect(phase).To(Equal("Running"))
		}, "2m", "5s").Should(Succeed())
	})
})

func vmPhaseAndIP(g Gomega, name string) (string, string) {
	out, err := utils.Run(exec.Command("kubectl", "get", "impvm", name, "-n", "default",
		"-o", "jsonpath={.status.phase} {.status.ip}"))
	g.Expect(err).NotTo(HaveOccurred())
	parts := strings.Fields(strings.TrimSpace(out))
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], ""
	default:
		return parts[0], parts[1]
	}
}

func vmNodeName(name string) string {
	out, err := utils.Run(exec.Command("kubectl", "get", "impvm", name, "-n", "default",
		"-o", "jsonpath={.status.nodeName}"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func agentPodOnNode(nodeName string) string {
	out, err := utils.Run(exec.Command("kubectl", "get", "pods", "-n", namespace,
		"-l", "app.kubernetes.io/component=agent",
		"--field-selector", "spec.nodeName="+nodeName,
		"-o", "jsonpath={.items[0].metadata.name}"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
