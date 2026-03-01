# Node Agent Design

**Date:** 2026-03-01
**Scope:** Node agent — VM lifecycle, VMDriver interface, OCI rootfs, DaemonSet config

---

## 1. Overview

The node agent runs as a DaemonSet pod on every node that carries the `imp/enabled=true`
label. It owns all Firecracker processes on its node. The operator never calls Firecracker
directly; it sets `spec.nodeName` on an `ImpVM` object and the agent on that node
reconciles the actual process.

**Key principle:** the agent is the sole writer of `status.phase` (runtime transitions),
`status.ip`, `status.runtimePID`, and `status.nodeName`. The operator writes scheduling
phases (Pending, Scheduled, Terminating, Failed on node loss) and `status.conditions`.

---

## 2. Architecture

```
cmd/agent/main.go
  └── agent.Agent.Run(ctx)
        └── controller-runtime Manager
              └── agent.ImpVMReconciler
                    └── VMDriver (interface)
                          ├── StubDriver      (envtest / IMP_STUB_DRIVER=true)
                          └── FirecrackerDriver (real hardware)
                                ├── firecracker-go-sdk
                                └── rootfs.Builder (go-containerregistry → ext4)
```

The reconciler is a standard controller-runtime reconciler — same framework as the
operator, different binary. Each agent pod filters to ImpVMs where
`spec.nodeName == NODE_NAME`. All others are ignored at the reconciler entry point.

---

## 3. VMDriver Interface

```go
// VMDriver abstracts the VM runtime backend.
// Implementations: StubDriver (testing), FirecrackerDriver (production).
type VMDriver interface {
    // Start boots the VM and returns its runtime PID.
    // Called when phase transitions Scheduled → Starting.
    Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (pid int64, err error)

    // Stop halts the VM. Blocks until stopped or ctx is cancelled.
    // Called when phase == Terminating.
    Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error

    // Inspect returns the current runtime state of the VM.
    // Called every reconcile to detect unexpected exits.
    Inspect(ctx context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error)
}

type VMState struct {
    Running bool
    IP      string
    PID     int64
}
```

The reconciler never imports `firecracker-go-sdk` directly. All Firecracker interaction
is behind this interface.

---

## 4. Reconcile Loop

```
Reconcile(ImpVM):
  1. If spec.nodeName != NODE_NAME → return (not ours)
  2. If phase == Terminating:
       Stop(vm)
       Clear spec.nodeName + status fields
       Return
  3. If phase == Scheduled:
       Set phase=Starting
       Start(vm) → pid, ip
       Set phase=Running, status.ip, status.runtimePID
  4. If phase == Running:
       Inspect(vm) → state
       If not running:
           lifecycle=ephemeral → phase=Succeeded, clear spec.nodeName + status
           lifecycle=persistent → phase=Failed
  5. All other phases (Pending etc.) → return, not our concern
```

### 4.1 Status field ownership

The agent writes these fields and no others:

| Field | Written when |
|---|---|
| `status.phase` | Starting, Running, Succeeded, Failed |
| `status.ip` | VM starts (Running) |
| `status.runtimePID` | VM starts (Running) |
| `status.nodeName` | Cleared on stop/failure (triggers operator finalizer removal) |

### 4.2 Requeue strategy

| Situation | Requeue |
|---|---|
| phase=Starting (waiting for boot) | 2s |
| phase=Running (steady state) | none — watch-driven |
| phase=Terminating (waiting for stop) | 2s |
| Driver error | return error → controller-runtime backoff |

---

## 5. StubDriver

Used in envtest and when `IMP_STUB_DRIVER=true` is set in the DaemonSet.

```go
type StubDriver struct {
    mu     sync.Mutex
    states map[string]*stubVM // keyed by "namespace/name"
}

type stubVM struct {
    running bool
    pid     int64
    ip      string
}
```

`Start` allocates a fake PID and IP and marks the VM running immediately.
`Stop` removes the entry.
`Inspect` returns the current entry, or `running=false` if not found.

The stub is safe for concurrent use and injectable via the same `VMDriver` interface —
zero test code changes when switching to `FirecrackerDriver`.

---

## 6. FirecrackerDriver

Wraps `firecracker-go-sdk`. Maintains an in-memory map of running machines on this node.

```go
type FirecrackerDriver struct {
    mu       sync.Mutex
    machines map[string]*firecracker.Machine // keyed by "namespace/name"
    cfg      FirecrackerConfig
}

type FirecrackerConfig struct {
    BinPath string // FC_BIN env var, default /usr/local/bin/firecracker
    SockDir string // FC_SOCK_DIR env var, default /run/imp/firecracker
    CacheDir string // IMP_IMAGE_CACHE env var, default /var/lib/imp/images
}
```

### 6.1 Start sequence

1. Build rootfs ext4 image from `spec.image` (see §7)
2. Create TAP device + attach to bridge for `spec.networkRef` (deferred — no-op in Phase 1)
3. Resolve assigned IP from `ImpNetwork` (deferred — loopback only in Phase 1)
4. Configure `firecracker.Config`: kernel, rootfs block device, TAP interface, VSOCK
5. `firecracker.NewMachine(ctx, cfg).Start(ctx)`
6. Store machine in map, return PID + IP

### 6.2 Stop sequence

1. `machine.Shutdown(ctx)` — graceful ACPI shutdown
2. On timeout → `machine.StopVMM()` — SIGKILL
3. Remove TAP device (deferred in Phase 1)
4. Remove socket file, delete from map

### 6.3 Inspect

Check if the machine is in the map and its process is still alive (`kill(pid, 0)`).

### 6.4 Phase 1 scope (OCI Ampere instance)

Networking steps (TAP/bridge/NAT) are no-ops in Phase 1. VMs boot with loopback only.
This validates the full Firecracker lifecycle (start/stop/status) on real hardware
before adding networking complexity.

---

## 7. OCI → ext4 Rootfs (rootfs.Builder)

No dependency on host containerd — uses `go-containerregistry` to pull layers directly.

```go
type Builder struct {
    CacheDir string // e.g. /var/lib/imp/images
}

// Build returns a path to a ready-to-use ext4 image for the given OCI reference.
// Results are cached by image digest — repeated calls with the same digest are instant.
func (b *Builder) Build(ctx context.Context, imageRef string) (string, error)
```

**Pipeline:**

```
1. Pull image manifest + config via go-containerregistry (registry auth from node's
   credential helpers if present)
2. Check cache: if <cacheDir>/<digest>.ext4 exists → return path immediately
3. Pull and extract layers into a temp overlay directory
4. Read CMD/ENTRYPOINT from OCI config → write to /sbin/init inside rootfs
5. Calculate required size (sum of layer sizes + headroom)
6. mkfs.ext4 -d <overlay-dir> -F <output.ext4> <size>
7. Move to <cacheDir>/<digest>.ext4
8. Return path
```

The `mkfs.ext4 -d` flag (populate from directory) avoids a separate copy step.
Cache keys are OCI content digests — cache is valid as long as the image digest matches.

---

## 8. DaemonSet Configuration

All paths injectable via environment variables. No hardcoded paths.

| Env var | Default | Purpose |
|---|---|---|
| `NODE_NAME` | — (required) | Filter ImpVMs; set via downward API fieldRef |
| `FC_BIN` | `/usr/local/bin/firecracker` | Firecracker binary (from Talos extension) |
| `FC_SOCK_DIR` | `/run/imp/firecracker` | Per-VM Unix socket directory |
| `IMP_IMAGE_CACHE` | `/var/lib/imp/images` | OCI layer + ext4 rootfs cache |
| `IMP_STUB_DRIVER` | `false` | Use StubDriver instead of FirecrackerDriver |

`cmd/agent/main.go` reads these, constructs the appropriate driver, injects it into the
reconciler. The stub flag lets the real binary run in a test cluster without KVM.

---

## 9. Package Structure

```
internal/agent/
    reconciler.go          ← ImpVM reconciler (watch + state machine)
    driver.go              ← VMDriver interface + VMState type
    stub_driver.go         ← StubDriver (envtest / CI)
    firecracker_driver.go  ← FirecrackerDriver (firecracker-go-sdk)
    rootfs/
        builder.go         ← OCI → ext4 (go-containerregistry + mkfs.ext4)
        builder_test.go    ← unit tests with a local registry
    suite_test.go          ← envtest setup
    reconciler_test.go     ← state machine tests using StubDriver
```

`rootfs` is a sub-package imported only by `FirecrackerDriver`. Tests in `internal/agent`
use `StubDriver` exclusively — no dependency on `mkfs.ext4` or filesystem tools in CI.

---

## 10. Implementation Phases

### Phase 1: Architecture + StubDriver (CI-testable, no KVM needed)
- `VMDriver` interface, `VMState`
- `StubDriver`
- `ImpVMReconciler` with full state machine
- Envtest suite: 4 test cases covering all state transitions
- `cmd/agent/main.go` driver selection via `IMP_STUB_DRIVER`

### Phase 2: FirecrackerDriver on real hardware (OCI Ampere ARM64)
- `FirecrackerDriver` (no networking — loopback only)
- `rootfs.Builder` (go-containerregistry → ext4)
- Spin up OCI free-tier ARM64 instance, install Firecracker binary, validate end-to-end

### Phase 3: Networking (TAP/bridge/NAT)
- TAP device management
- Bridge setup per ImpNetwork
- nftables/iptables NAT rules (CNI-aware)
- IP allocation from ImpNetwork subnet
