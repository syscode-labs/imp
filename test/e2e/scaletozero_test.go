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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/syscode-labs/imp/test/utils"
)

// These cover the Phase 3 ScaleToZero API surface (the new desiredState mode +
// idleTimeout) as installed by the chart CRDs and enforced by the validating
// webhook. They are control-plane only, so they run in the agent-off Kind smoke
// harness. The real wake-on-traffic datapath is exercised separately — see the
// Pending spec in scaletozero_datapath_test.go, which needs a KVM node + a real
// Firecracker agent.
var _ = Describe("Imp ScaleToZero API", Label("smoke"), func() {
	const vmName = "e2e-sz-vm"

	AfterEach(func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "impvm", vmName, "-n", "default", "--ignore-not-found"))
	})

	scaleToZeroManifest := func(idleTimeout string) string {
		idle := ""
		if idleTimeout != "" {
			idle = "\n  idleTimeout: " + idleTimeout
		}
		return fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: %s
  namespace: default
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
  desiredState: ScaleToZero%s
`, vmName, idle)
	}

	It("rejects a ScaleToZero ImpVM whose idleTimeout is below the 10s floor", func() {
		apply := exec.Command("kubectl", "apply", "-f", "-")
		apply.Stdin = strings.NewReader(scaleToZeroManifest("5s"))
		out, err := utils.Run(apply)
		Expect(err).To(HaveOccurred(), "sub-floor idleTimeout should be rejected by the webhook")
		Expect(out).To(ContainSubstring("idleTimeout must be at least 10s"))
	})

	It("admits a ScaleToZero ImpVM and returns the experimental warning", func() {
		apply := exec.Command("kubectl", "apply", "-f", "-")
		apply.Stdin = strings.NewReader(scaleToZeroManifest("30s"))
		out, err := utils.Run(apply)
		Expect(err).NotTo(HaveOccurred())
		// kubectl prints admission warnings to stderr; utils.Run uses CombinedOutput.
		Expect(out).To(ContainSubstring("ScaleToZero is experimental"))
	})
})
