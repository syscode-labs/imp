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
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/syscode-labs/imp/test/utils"
)

const (
	namespace      = "imp-system"
	helmRelease    = "imp"
	helmCRDRelease = "imp-crds"
)

func getenvOrDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func configureEventuallyDefaults() {
	if timeoutRaw := os.Getenv("IMP_E2E_EVENTUALLY_TIMEOUT"); timeoutRaw != "" {
		timeout, err := time.ParseDuration(timeoutRaw)
		Expect(err).NotTo(HaveOccurred(), "invalid IMP_E2E_EVENTUALLY_TIMEOUT")
		SetDefaultEventuallyTimeout(timeout)
	}

	if intervalRaw := os.Getenv("IMP_E2E_EVENTUALLY_POLL_INTERVAL"); intervalRaw != "" {
		interval, err := time.ParseDuration(intervalRaw)
		Expect(err).NotTo(HaveOccurred(), "invalid IMP_E2E_EVENTUALLY_POLL_INTERVAL")
		SetDefaultEventuallyPollingInterval(interval)
	}
}

// Note: the Kind cluster itself is managed by the CI workflow (helm/kind-action@v1) or
// must be created manually before running these tests locally:
//
//	kind create cluster --name imp-e2e
//	go test -tags e2e ./test/e2e/... -v -timeout 15m
//	kind delete cluster --name imp-e2e

var _ = BeforeSuite(func() {
	configureEventuallyDefaults()

	By("creating namespace")
	nsCmd := exec.Command("kubectl", "create", "ns", namespace)
	_, _ = utils.Run(nsCmd) // ignore if already exists

	if os.Getenv("IMP_E2E_REAL_AGENT") == "true" {
		By("labeling nodes for agent scheduling")
		nodes, err := utils.Run(exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[*].metadata.name}"))
		Expect(err).NotTo(HaveOccurred())
		for _, node := range strings.Fields(nodes) {
			_, _ = utils.Run(exec.Command("kubectl", "taint", "nodes", node,
				"node-role.kubernetes.io/control-plane:NoSchedule-"))
			_, err = utils.Run(exec.Command("kubectl", "label", "node", node, "imp/enabled=true", "--overwrite"))
			Expect(err).NotTo(HaveOccurred())
		}
	}

	By("installing cert-manager")
	Expect(utils.InstallCertManager()).To(Succeed(), "helm install cert-manager failed")

	By("installing imp-crds chart")
	crdsCmd := exec.Command("helm", "install", helmCRDRelease, "charts/imp-crds",
		"--namespace", namespace, "--wait", "--create-namespace")
	_, err := utils.Run(crdsCmd)
	Expect(err).NotTo(HaveOccurred(), "helm install imp-crds failed")

	By("installing imp chart")
	operatorRepo := getenvOrDefault("IMP_E2E_OPERATOR_IMAGE_REPOSITORY", "local/imp-operator")
	operatorTag := getenvOrDefault("IMP_E2E_OPERATOR_IMAGE_TAG", "e2e")
	agentRepo := getenvOrDefault("IMP_E2E_AGENT_IMAGE_REPOSITORY", "local/imp-agent")
	agentTag := getenvOrDefault("IMP_E2E_AGENT_IMAGE_TAG", "e2e")
	runtimeRepo := getenvOrDefault("IMP_E2E_RUNTIME_IMAGE_REPOSITORY", "local/imp-agent")
	runtimeTag := getenvOrDefault("IMP_E2E_RUNTIME_IMAGE_TAG", "e2e")

	impArgs := []string{"install", helmRelease, "charts/imp",
		"--namespace", namespace,
		"--set", "operator.image.repository=" + operatorRepo,
		"--set", "operator.image.tag=" + operatorTag,
		"--set", "agent.image.repository=" + agentRepo,
		"--set", "agent.image.tag=" + agentTag,
		"--set", "runtime.image.repository=" + runtimeRepo,
		"--set", "runtime.image.tag=" + runtimeTag,
		"--set", "agent.env.kernelPath=/var/lib/imp/vmlinux",
		"--set", "metrics.serviceMonitor.enabled=false",
		"--set", "metrics.podMonitor.enabled=false",
	}
	if os.Getenv("IMP_E2E_REAL_AGENT") == "true" {
		// Real KVM runner: let the agent schedule (no no-agent nodeSelector)
		// and enable the scale-to-zero datapath under test.
		impArgs = append(impArgs,
			"--set", "agent.extraEnv[0].name=IMP_SCALE_TO_ZERO",
			"--set-string", "agent.extraEnv[0].value=true",
			// Firecracker runs as a child process of the agent, so guest RAM counts
			// against the agent container's own memory cgroup. The chart default
			// (128Mi) assumes no hosted VMs and OOM-kills almost immediately once a
			// real microVM boots — silently, since a SIGKILL leaves no panic/log line.
			"--set", "agent.resources.limits.memory=1Gi",
		)
	} else {
		impArgs = append(impArgs, "--set-string", "agent.nodeSelector.imp\\.dev/no-agent=true")
	}
	impArgs = append(impArgs, "--wait", "--timeout", "10m")

	impCmd := exec.Command("helm", impArgs...)
	_, err = utils.Run(impCmd)
	Expect(err).NotTo(HaveOccurred(), "helm install imp failed")
})

var _ = AfterSuite(func() {
	if os.Getenv("IMP_E2E_REAL_AGENT") == "true" {
		By("dumping pod status + agent/operator logs before teardown")
		podStatus := exec.Command("kubectl", "get", "pods", "-n", namespace, "-o", "wide")
		out, _ := utils.Run(podStatus)
		fmt.Println("--- pod status (check RESTARTS) ---\n" + out)

		agentLogsPrev := exec.Command("kubectl", "logs", "-n", namespace,
			"-l", "app.kubernetes.io/component=agent", "--tail=500", "--prefix", "--previous")
		out, _ = utils.Run(agentLogsPrev)
		fmt.Println("--- agent logs (previous container, if it restarted) ---\n" + out)

		agentLogs := exec.Command("kubectl", "logs", "-n", namespace,
			"-l", "app.kubernetes.io/component=agent", "--tail=500", "--prefix")
		out, _ = utils.Run(agentLogs)
		fmt.Println("--- agent logs ---\n" + out)

		operatorLogs := exec.Command("kubectl", "logs", "-n", namespace,
			"-l", "app.kubernetes.io/component=operator", "--tail=500", "--prefix")
		out, _ = utils.Run(operatorLogs)
		fmt.Println("--- operator logs ---\n" + out)
	}

	By("uninstalling imp chart")
	unimpCmd := exec.Command("helm", "uninstall", helmRelease, "--namespace", namespace)
	_, _ = utils.Run(unimpCmd)

	By("uninstalling imp-crds chart")
	uncrdsCmd := exec.Command("helm", "uninstall", helmCRDRelease, "--namespace", namespace)
	_, _ = utils.Run(uncrdsCmd)

	By("uninstalling cert-manager")
	utils.UninstallCertManager()
})

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Imp E2E Suite")
}
