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
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/syscode-labs/imp/test/utils"
)

const (
	namespace      = "imp-system"
	helmRelease    = "imp"
	helmCRDRelease = "imp-crds"
)

var _ = BeforeSuite(func() {
	By("creating namespace")
	nsCmd := exec.Command("kubectl", "create", "ns", namespace)
	_, _ = utils.Run(nsCmd) // ignore if already exists

	By("installing cert-manager")
	Expect(utils.InstallCertManager()).To(Succeed(), "helm install cert-manager failed")

	By("installing imp-crds chart")
	crdsCmd := exec.Command("helm", "install", helmCRDRelease, "charts/imp-crds",
		"--namespace", namespace, "--wait", "--create-namespace")
	_, err := utils.Run(crdsCmd)
	Expect(err).NotTo(HaveOccurred(), "helm install imp-crds failed")

	By("installing imp chart")
	impCmd := exec.Command("helm", "install", helmRelease, "charts/imp",
		"--namespace", namespace,
		"--set", "metrics.serviceMonitor.enabled=false",
		"--set", "metrics.podMonitor.enabled=false",
		"--wait", "--timeout", "2m")
	_, err = utils.Run(impCmd)
	Expect(err).NotTo(HaveOccurred(), "helm install imp failed")
})

var _ = AfterSuite(func() {
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
