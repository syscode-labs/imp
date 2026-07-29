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
  image: docker.io/library/nginx:alpine
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
  image: docker.io/library/nginx:alpine
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

		By("warming the pinger's ARP cache for the target before it suspends")
		// The target's TAP is torn down on suspend (frees memory) — only the VTEP
		// is kept, so cross-node overlay routing still works, but the pinger's own
		// ARP resolution for the target's IP can only happen while the target's TAP
		// exists to answer it. A sender with a warm ARP entry sends the IP frame
		// directly on wake with no fresh ARP round-trip needed; a cold sender never
		// gets an IP frame onto the wire in the first place, so the AF_PACKET wake
		// hook (bound to ETH_P_IP, not ARP) never fires. This mirrors the real
		// intended use case: traffic to an already-known, now-idle destination.
		// Retry: status.phase==Running reflects the host starting Firecracker, not
		// that the guest kernel has finished booting and brought its network up —
		// an immediate single-shot ping can race a guest that isn't ready yet.
		var warmOut string
		Eventually(func(g Gomega) {
			warmExit, out, execErr := execInVM(pingerName, "ping", "-c", "1", "-W", "5", targetIP)
			warmOut = out
			g.Expect(execErr).NotTo(HaveOccurred())
			g.Expect(warmExit).To(Equal(int32(0)))
		}, "1m", "3s").Should(Succeed(), func() string {
			return "ARP warm-up ping never succeeded while target was still Running:\n" + warmOut
		})

		By("waiting for the target to auto-suspend after going idle")
		Eventually(func(g Gomega) {
			phase, _ := vmPhaseAndIP(g, targetName)
			g.Expect(phase).To(Equal("Suspended"))
		}, "2m", "5s").Should(Succeed())

		By("pinging the suspended VM's overlay IP from the pinger VM's guest agent")
		wakeExit, wakeOut, err := execInVM(pingerName, "ping", "-c", "3", "-W", "2", targetIP)
		Expect(err).NotTo(HaveOccurred())
		GinkgoWriter.Printf("wake ping exit=%d output:\n%s\n", wakeExit, wakeOut)

		By("asserting the target wakes: Suspended -> Resuming -> Running")
		Eventually(func(g Gomega) {
			phase, _ := vmPhaseAndIP(g, targetName)
			g.Expect(phase).To(Equal("Running"))
		}, "2m", "5s").Should(Succeed())
	})
})

// execInVM runs command inside vmName's guest via the agent's vsock guest-exec
// API (assumes a port-forward to localhost:19091 is already active) and returns
// the process exit code plus the combined stdout+stderr NDJSON stream decoded to
// plain text.
func execInVM(vmName string, command ...string) (int32, string, error) {
	body, err := json.Marshal(map[string][]string{"command": command})
	if err != nil {
		return 0, "", err
	}
	resp, err := http.Post( //nolint:noctx
		fmt.Sprintf("http://localhost:19091/v1/exec/default/%s", vmName),
		"application/json", strings.NewReader(string(body)))
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("exec %s: unexpected status %d", vmName, resp.StatusCode)
	}

	var out strings.Builder
	var exitCode int32
	dec := json.NewDecoder(resp.Body)
	for dec.More() {
		var line struct {
			Stream string `json:"stream"`
			Line   string `json:"line,omitempty"`
			Code   *int32 `json:"code,omitempty"`
		}
		if err := dec.Decode(&line); err != nil {
			return 0, out.String(), fmt.Errorf("decode exec response: %w", err)
		}
		switch line.Stream {
		case "exit":
			if line.Code != nil {
				exitCode = *line.Code
			}
		default:
			out.WriteString(line.Line)
			out.WriteString("\n")
		}
	}
	return exitCode, out.String(), nil
}

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
	// status.nodeName exists on the CRD but nothing in the codebase ever writes it
	// (only spec.nodeName is set, by the scheduler) — read spec instead.
	out, err := utils.Run(exec.Command("kubectl", "get", "impvm", name, "-n", "default",
		"-o", "jsonpath={.spec.nodeName}"))
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
