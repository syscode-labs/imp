# Imp — Current Status

**Last updated:** 2026-03-01
**Audience:** Personal reference

---

## What's Built

### API (`api/v1alpha1`)
- [x] All 6 CRDs — `ImpVM`, `ImpVMClass`, `ImpVMTemplate`, `ImpNetwork`, `ClusterImpConfig`, `ClusterImpNodeProfile`
- [x] `HTTPCheckSpec` on both `ProbeSpec` (per-VM) and `ClusterImpConfigSpec` (global default)
- [x] Full type tests for all 6 CRDs + shared types

### Operator (`internal/controller`)
- [x] Full reconcile loop: finalizer → schedule → syncStatus
- [x] Scheduler: `imp/enabled=true` nodes, `nodeSelector` filter, `ClusterImpNodeProfile` cap, least-loaded with alphabetical tie-break
- [x] Status sync: node health check, reschedule ephemeral / fail persistent on node loss
- [x] Conditions: `Scheduled`, `Ready`, `NodeHealthy`
- [x] Deletion + finalizer: Terminating signal to agent, 2-min force-remove timeout
- [x] Operator HTTP probe (opt-in): poll `status.ip`, failure threshold, recovery events
- [x] Node watch → reactive reconcile on node events
- [x] All 8 event reasons
- [x] Test coverage: **84.3%**

### Agent (`internal/agent`)
- [x] `VMDriver` interface + `VMState`
- [x] `StubDriver` with one-shot error injection (`InjectStartError`, `InjectStopError`, `InjectInspectError`)
- [x] `ImpVMReconciler`: full state machine across all 7 phases
- [x] Wired into controller-runtime manager (`cmd/main.go`)
- [x] env-var config: `NODE_NAME`, `FC_BIN`, `FC_SOCK_DIR`, `IMP_IMAGE_CACHE`, `IMP_STUB_DRIVER`
- [x] Test coverage: **90.1%**

### Infrastructure
- [x] Kind e2e suite (stub driver, no KVM needed)
- [x] golangci-lint config + pre-commit hooks
- [x] GitHub Actions CI

---

## What's Next

### Immediate (Phase 1 completion)

1. **`FirecrackerDriver`** (`internal/agent/firecracker_driver.go`)
   - Wrap `firecracker-go-sdk`
   - Start: build rootfs ext4 → configure VM → `machine.Start(ctx)` → store PID
   - Stop: graceful ACPI → SIGKILL timeout → cleanup socket
   - Inspect: `kill(pid, 0)` liveness check
   - Validate on OCI Ampere ARM64 instance (loopback only, no TAP in Phase 1)

2. **`rootfs.Builder`** (`internal/agent/rootfs/builder.go`)
   - `go-containerregistry` to pull OCI layers (no containerd dependency)
   - `mkfs.ext4 -d` to assemble rootfs without a copy step
   - Cache by OCI digest at `IMP_IMAGE_CACHE`

3. **Admission webhooks** — validation + defaulting for ImpVM, ImpVMClass, ImpVMTemplate

4. **CNI detection** — startup signal via CRD presence or DaemonSet scan; store in operator state; emit `CNIDetected` event

5. **`ImpNetwork` controller** — bridge + NAT rule management per network object

6. **Talos system extension** — package Firecracker binary as OCI image, multi-arch manifest, publish to GHCR

7. **DaemonSet + Operator manifests** — Helm chart, RBAC (minimal profile default)

### Later (Phase 2+)

- Persistent VM lifecycle
- VM snapshot/restore (migration + warm pools)
- Cilium IPAM delegation
- CI runner mode
- Cross-node VM networking

---

## Open Decisions

| Decision | Options | Status |
|----------|---------|--------|
| Phase 1 networking scope | Loopback only first (validate FC on real hardware), then TAP/bridge | **Decided: loopback first** |
| VSOCK guest agent | Build from scratch vs. use existing (e.g. `kata-agent` subset) | Open — deferred to Phase 1 hardware work |
| Rootfs init script | Write CMD/ENTRYPOINT to `/sbin/init` vs. use a supervisor | Open |
| OCI registry auth | Node credential helpers only vs. `imagePullSecrets` | Open — credential helpers first |
| Warm VM pools | Phase 2 snapshot-based vs. pre-booted live VMs | Deferred to Phase 2 |
| `imp.dev` domain | Register for label/annotation use or keep `imp/` prefix forever | Deferred — keep `imp/` prefix for now |

---

## Known Gaps

- **`ImpNetwork` controller is a stub** — no actual bridge or NAT logic yet
- **VSOCK probes not implemented** — `startupProbe`, `readinessProbe`, `livenessProbe` in the spec but the agent doesn't run them (deferred to FirecrackerDriver work)
- **`ImpVMTemplate` resolution not implemented** — the operator doesn't yet merge template fields into the ImpVM before scheduling
- **Compute headroom formula not enforced** — only `maxImpVMs` hard cap is used; `capacityFraction` × allocatable CPU/mem calculation is not wired up
- **README.md is the kubebuilder default** — needs to be replaced with actual project documentation

---

## Infrastructure Notes

- OCI account available for temporary ARM64 VMs — stay within free tier (PAYG account)
- Firecracker binary must come from the Talos extension; don't install it ad-hoc on test nodes
- `KUBEBUILDER_ASSETS` for local test runs: `/Users/giovanni/syscode/git/imp/bin/k8s/k8s/1.35.0-darwin-amd64`
