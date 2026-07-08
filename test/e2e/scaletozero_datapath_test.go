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
	. "github.com/onsi/ginkgo/v2"
)

// Wake-on-traffic datapath e2e — DELIBERATELY Pending (PIt).
//
// This validates the one thing the host-only unit + envtest coverage cannot: that
// the agent's AF_PACKET hook actually observes the first frame destined to a
// TAP-less (suspended) ScaleToZero VM on imp's VXLAN overlay, and that a real
// Firecracker suspend/resume round-trips. It is UNVALIDATED and cannot run yet —
// see docs (runner runbook) for why the harness must change before it can.
//
// Prerequisites that do NOT exist in the current e2e harness (all are runbook
// decisions, which is why this spec stays Pending until the runbook lands them):
//
//  1. A KVM-capable node. BeforeSuite currently pins the agent to
//     nodeSelector imp.dev/no-agent=true (agent off) and the Kind smoke runner has
//     no /dev/kvm. A real Firecracker agent needs nested virt + the Firecracker
//     Talos system extension + a guest kernel at agent.env.kernelPath.
//  2. The agent enabled with IMP_SCALE_TO_ZERO=true. BeforeSuite must conditionally
//     drop the no-agent selector and set the env when targeting the KVM runner
//     (e.g. gate on an IMP_E2E_REAL_AGENT env var).
//  3. A traffic source on the same ImpNetwork. OPEN QUESTION for first run: how does
//     a frame reach a suspended VM's overlay IP? The realistic source is a second
//     always-on VM on the same ImpNetwork pinging the suspended VM — but that pulls
//     in guest exec / vsock. This is the crux to resolve when the runner is live.
//
// Intended flow once the harness supports it:
//   - Create an ImpNetwork + a ScaleToZero ImpVM with a short idleTimeout (e.g. 15s).
//   - Wait for Running, then (no traffic) wait for the agent's idle detector to
//     auto-suspend it → status.phase == Suspended, VTEP retained.
//   - Send a frame to the VM's overlay IP from the traffic source.
//   - Assert the VM returns to Running (Suspended → Resuming → Running).
var _ = Describe("Imp ScaleToZero datapath", Label("datapath"), func() {
	PIt("wakes a suspended ScaleToZero VM when a frame arrives for its overlay IP", func() {
		Skip("requires a KVM node + real Firecracker agent; see the e2e runner runbook")
	})
})
