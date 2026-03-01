# Imp — Feature Requirements

**Audience:** Contributors and maintainers
**API group:** `imp.dev/v1alpha1`
**Repo:** `github.com/syscode-labs/imp`

For architecture rationale see [`docs/plans/2026-03-01-imp-design.md`](plans/2026-03-01-imp-design.md).

---

## Architecture Layers

```
Layer 3: User (kubectl apply -f myvm.yaml)
Layer 2: Operator (Deployment, 1 replica) — schedules VMs, manages conditions
Layer 1: Node Agent (DaemonSet) — owns Firecracker processes on each node
Layer 0: Talos Node — KVM + Firecracker binary via system extension
```

**Key invariant:** the operator never calls Firecracker. It sets `spec.nodeName`; the agent on that node reconciles the actual process. This mirrors how the kubelet handles Pods.

**Status field ownership is split:** the operator writes `status.conditions` and scheduling phases (Pending, Scheduled, Terminating, Failed-on-node-loss); the agent writes runtime phases (Starting, Running, Succeeded), `status.ip`, and `status.runtimePID`.

---

## CRDs

All resources are under `imp.dev/v1alpha1`.

| Kind | Short | Scope | Purpose |
|------|-------|-------|---------|
| `ImpVM` | `impvm` | Namespace | A microVM instance |
| `ImpVMTemplate` | `impvmt` | Namespace | Reusable VM definition |
| `ImpNetwork` | `impnet` | Namespace | Network topology (NAT, subnet) |
| `ImpVMClass` | `impcls` | Cluster | Compute profiles (vCPU, mem, disk) |
| `ClusterImpConfig` | `impcfg` | Cluster | Operator-wide settings — singleton named `cluster` |
| `ClusterImpNodeProfile` | `impnp` | Cluster | Per-node capacity overrides — name matches node name |

### Inheritance chains

```
Compute:  ImpVMClass.(vcpu/mem/disk) → ImpVMTemplate.(overrides) → ImpVM.(overrides)
Probes:   ImpVMClass.spec.probes → ImpVMTemplate.spec.probes → ImpVM.spec.probes
Network:  ImpNetwork ← referenced by ImpVMTemplate or ImpVM
Capacity: ClusterImpConfig.defaultFraction ← overridden by ClusterImpNodeProfile.capacityFraction
```

---

## Design Principles

1. **Declarative over imperative** — all behaviour driven by CRD spec, not ad-hoc API calls
2. **Operator ↔ Agent split** — operator owns scheduling; agent owns process lifecycle. No shared mutable state between them — the ImpVM object IS the interface
3. **VMDriver interface** — agent business logic never imports `firecracker-go-sdk` directly; all Firecracker interaction is behind `VMDriver`. Enables full test coverage without KVM
4. **OCI-native** — VMs are defined by OCI images, exactly like containers. No cloud-init required for standard use cases
5. **YAGNI** — features are built when needed for a real use case, not speculatively
6. **Talos-first** — design for Talos Linux immutability; SSH-less operation

---

## Phase 1 Requirements — Ephemeral VMs

### Operator (Layer 2)

- [x] Add `imp/finalizer` to every new ImpVM
- [x] Schedule VMs to nodes with `imp/enabled=true` label
- [x] Filter by `spec.nodeSelector`
- [x] Enforce `ClusterImpNodeProfile.spec.maxImpVMs` capacity cap
- [x] Least-loaded node selection; alphabetical tie-break
- [x] Set `status.conditions`: `Scheduled`, `Ready`, `NodeHealthy`
- [x] Detect node loss — reschedule ephemeral VMs; fail persistent VMs
- [x] Handle deletion: set `phase=Terminating`, wait for agent, 2-min force-remove timeout
- [x] Operator HTTP probe (opt-in) — poll VM's health endpoint, update `Ready` condition
- [x] Emit events: `Scheduled`, `Unschedulable`, `NodeLost`, `Rescheduling`, `Terminating`, `TerminationTimeout`, `HealthCheckFailed`, `HealthCheckRecovered`
- [x] Reactive reconcile on Node events via watch → map to assigned ImpVMs
- [ ] `globalHTTPCheck` from `ClusterImpConfig` — in code, needs e2e validation
- [ ] Admission webhooks (validation + defaulting)
- [ ] CNI detection
- [ ] `ImpNetwork` controller

### Node Agent (Layer 1)

- [x] `VMDriver` interface + `VMState` type
- [x] `StubDriver` — thread-safe fake for testing/CI (`IMP_STUB_DRIVER=true`)
- [x] `ImpVMReconciler` — full state machine: Scheduled→Starting→Running→Succeeded/Failed, Terminating→cleanup
- [x] Filter to `spec.nodeName == NODE_NAME` at reconciler entry
- [x] Wire into controller-runtime manager, configured via env vars
- [ ] `FirecrackerDriver` — real Firecracker process management
- [ ] `rootfs.Builder` — OCI image → ext4 disk image via `go-containerregistry` + `mkfs.ext4`
- [ ] Image cache by OCI digest
- [ ] VSOCK guest agent probes (startupProbe, readinessProbe, livenessProbe)
- [ ] TAP device + bridge management per `ImpNetwork`
- [ ] nftables/iptables NAT rules (CNI-aware)
- [ ] IP allocation from `ImpNetwork` subnet

### Infrastructure

- [ ] Talos system extension — Firecracker binary packaged as OCI image
- [ ] DaemonSet manifest with `hostPath: /dev/kvm` + downward API `NODE_NAME`
- [ ] Operator `Deployment` manifest
- [ ] RBAC manifests (CRD-based CNI detection; minimal default profile)
- [ ] Helm chart
- [x] Kind e2e test suite (stub driver)
- [ ] Prometheus metrics + Grafana dashboard ConfigMap
- [ ] OpenTelemetry traces (opt-in, off by default)

---

## Phase 2 Requirements (future)

- Persistent VM lifecycle (restart, state tracking across node reboots)
- VM migration via Firecracker snapshot/restore
- Warm VM pools (pre-booted snapshots for near-instant assignment)
- Cilium IPAM delegation (`CiliumPodIPPool`)

## Phase 3 Requirements (future)

- CI runner mode (`ImpVMTemplate` with runner label + auto-registration)
- Cross-node VM networking (VXLAN overlay)
- Cilium external workloads (NetworkPolicy enforcement on VMs, Hubble visibility)

---

## Non-Goals (v1)

- **VM live migration** — snapshot/restore planned for Phase 2
- **Windows guests** — Linux only
- **Managed Kubernetes** (EKS, GKE, AKS) — no `/dev/kvm` access
- **Multi-cluster** — single cluster only
- **GPU passthrough** — not a Firecracker feature
- **Nested virtualisation** — Firecracker requires real KVM

---

## Label and Annotation Conventions

| Key | Used on | Purpose |
|-----|---------|---------|
| `imp/enabled=true` | Node | Opt-in: node accepts ImpVM workloads |
| `imp/finalizer` | ImpVM | Finalizer name |
| `imp/httpcheck-failures` | ImpVM annotation | Running count of consecutive HTTP probe failures |

Note: CRD API group is `imp.dev/v1alpha1` (established in Go types). Labels/annotations use `imp/` prefix (domain not yet registered, `imp.dev` reserved for the API group).

---

## Testing Strategy

| Layer | Approach |
|-------|----------|
| Unit | `envtest` (real API server + etcd, no cluster) — Ginkgo + Gomega |
| E2E | Kind cluster + StubDriver for CI; real Talos node for hardware validation |
| Driver isolation | `VMDriver` interface means all reconciler logic is testable without KVM |

Coverage targets: agent ≥ 90%, controller ≥ 70% (currently: 90.1% / 84.3%).
