# Imp — Phase 2 Design

> *Persistent VMs, advanced networking, warm pools, and migration*

**Goal:** Extend Imp beyond ephemeral sandboxes into persistent, resilient microVMs with
production-grade networking, near-instant cold-start via warm pools, and live migration.

**Scope:** Four independent areas tackled in order. Each is self-contained and can ship
separately.

---

## 1. Persistent VM Lifecycle

### Problem

Ephemeral VMs disappear on failure. Persistent workloads (dev environments, long-running
agents, CI workers) need automatic restart with controlled retry behaviour.

### Design

#### `restartPolicy` field

Added to `ImpVMClass`, `ImpVMTemplate`, and `ImpVM`. Inherits in that order: class →
template → VM (VM wins if set, otherwise template, otherwise class).

```go
type RestartPolicy struct {
    // Mode controls where the VM restarts.
    // "in-place" restarts on the same node; "reschedule" re-runs the scheduler.
    // +kubebuilder:default=in-place
    // +kubebuilder:validation:Enum=in-place;reschedule
    Mode string `json:"mode,omitempty"`

    Backoff RestartBackoff `json:"backoff,omitempty"`

    // OnExhaustion controls behaviour once maxRetries is reached.
    // +kubebuilder:default=fail
    // +kubebuilder:validation:Enum=fail;manual-reset;cool-down
    OnExhaustion string `json:"onExhaustion,omitempty"`

    // CoolDownPeriod is the duration to wait before auto-resetting the retry counter
    // when onExhaustion is "cool-down".
    // +kubebuilder:default="1h"
    CoolDownPeriod string `json:"coolDownPeriod,omitempty"`
}

type RestartBackoff struct {
    // +kubebuilder:default=5
    MaxRetries int32 `json:"maxRetries,omitempty"`
    // +kubebuilder:default="10s"
    InitialDelay string `json:"initialDelay,omitempty"`
    // +kubebuilder:default="5m"
    MaxDelay string `json:"maxDelay,omitempty"`
}
```

**Defaults:** in-place, 5 retries, 10 s initial delay, 5 m cap, fail on exhaustion.

#### Retry state in `ImpVMStatus`

```go
// RestartCount is the number of times this VM has been restarted.
RestartCount int32 `json:"restartCount,omitempty"`

// NextRetryAfter is the earliest time the controller will attempt a restart.
// +optional
NextRetryAfter *metav1.Time `json:"nextRetryAfter,omitempty"`
```

#### Mechanism

Controller reconciles a failed VM:

1. If `restartPolicy` is not set or `onExhaustion: fail` with count exceeded → set phase
   `Failed`, stop.
2. Compute next delay: `min(initialDelay * 2^restartCount, maxDelay)`.
3. Set `nextRetryAfter = now + delay`, increment `restartCount`.
4. Return `ctrl.Result{RequeueAfter: delay}`.
5. On next reconcile, if `now >= nextRetryAfter`: restart VM (in-place or reschedule).

#### `onExhaustion` behaviours

| Value | Behaviour |
|---|---|
| `fail` | VM enters `Failed` phase permanently. |
| `manual-reset` | VM enters `RetryExhausted` phase; user adds annotation `imp/reset-retries: "true"` to clear counter and resume. |
| `cool-down` | After `coolDownPeriod` elapses with no further failures, counter resets automatically. |

#### Inheritance resolution

The controller merges policies at admission time (defaulter webhook). Result stored in
`ImpVM.Spec.RestartPolicy` so the controller never needs to chase references at runtime.

---

## 2. Networking & IPAM

### Problem

All VMs currently share one internal subnet with NAT. Production workloads need isolation,
group-level L2/L3 connectivity, and optional Cilium-native addressing.

### Design

#### Isolation-first model

Each VM is isolated by default. Isolation means:

- Dedicated `/30` CIDR (2 usable IPs: VM + gateway).
- No L2 adjacency with other VMs unless explicitly grouped.
- NAT egress via the host's default interface.

#### `networkGroup` field

Added to `ImpVMTemplate` and `ImpVM`. VMs with the same `networkGroup` value on the same
`ImpNetwork` share a subnet.

```go
type NetworkGroupSpec struct {
    // Name identifies the group within the ImpNetwork.
    Name string `json:"name"`

    // Connectivity defines how group members can reach each other.
    // +kubebuilder:default=subnet
    // +kubebuilder:validation:Enum=subnet;policy-only
    Connectivity string `json:"connectivity,omitempty"`

    // ExpectedSize is a hint for CIDR sizing. Controller rounds up to the next
    // power-of-2 subnet. Default: 14 (yields /28).
    // +kubebuilder:default=14
    ExpectedSize int32 `json:"expectedSize,omitempty"`
}
```

CIDR sizing table (examples):

| `expectedSize` | Allocated CIDR |
|---|---|
| 1 (isolated) | `/30` |
| 2–14 | `/28` |
| 15–62 | `/26` |
| 63–254 | `/24` |

#### IPAM provider

```go
type IPAMSpec struct {
    // Provider selects the IPAM backend.
    // +kubebuilder:default=internal
    // +kubebuilder:validation:Enum=internal;cilium
    Provider string `json:"provider,omitempty"`

    // Cilium configures Cilium IPAM. Required when provider is "cilium".
    // +optional
    Cilium *CiliumIPAMSpec `json:"cilium,omitempty"`
}

type CiliumIPAMSpec struct {
    // PoolRef is the name of the CiliumPodIPPool to allocate from.
    PoolRef string `json:"poolRef"`
}
```

`ipam` lives on `ImpNetwork`. All VMs and groups on that network use the same provider.

**Cilium IPAM behaviour:** Controller allocates an IP from the named `CiliumPodIPPool`
via the Cilium IPAM API. Imp creates TAP/bridge infrastructure and passes the allocated
IP to the VM. Cilium owns routing, policy enforcement, and subnet visibility. Imp's
internal allocator is bypassed for this network.

**Controller startup log:** The operator logs detected Cilium CRDs at startup regardless
of configured IPAM provider — informational only.

**Deferred:** `CiliumExternalWorkload` (requires `cilium-agent` inside each VM — too
heavy for ephemeral microVMs). Planned as a future opt-in for long-lived VMs.

---

## 3. Warm VM Pools

### Problem

Firecracker boot time is sub-second but not zero. Workloads that need instant response
(CI job dispatch, serverless functions) benefit from pre-booted VMs waiting in a pool.

### Design

#### Snapshot CRD

```go
type ImpVMSnapshot struct {
    Spec   ImpVMSnapshotSpec   `json:"spec"`
    Status ImpVMSnapshotStatus `json:"status"`
}

type ImpVMSnapshotSpec struct {
    // SourceVM is the namespace/name of the running ImpVM to snapshot.
    SourceVM types.NamespacedName `json:"sourceVM"`

    // Schedule is a cron expression for recurring snapshots (optional).
    // e.g. "0 2 * * *" — daily at 02:00.
    // +optional
    Schedule string `json:"schedule,omitempty"`

    // Retention controls how many snapshots to keep.
    // +kubebuilder:default=3
    Retention int32 `json:"retention,omitempty"`

    // Storage defines where the snapshot artifact is persisted.
    Storage SnapshotStorageSpec `json:"storage"`
}

type SnapshotStorageSpec struct {
    // Type selects the storage backend.
    // +kubebuilder:validation:Enum=node-local;oci-registry
    Type string `json:"type"`

    // OCIRegistry configures an OCI registry destination.
    // Required when type is "oci-registry".
    // +optional
    OCIRegistry *OCIRegistrySpec `json:"ociRegistry,omitempty"`
}

type OCIRegistrySpec struct {
    // Repository is the full image reference prefix (e.g. ghcr.io/org/imp-snapshots).
    Repository string `json:"repository"`
    // PullSecretRef references a Secret with registry credentials.
    // +optional
    PullSecretRef *corev1.LocalObjectReference `json:"pullSecretRef,omitempty"`
}
```

**Velero compatibility:** The `storage.ociRegistry.repository` field provides a stable,
explicit location for backup tools. Velero or similar can discover and back up OCI
artifacts at the declared repository. A dedicated Velero plugin is a future enhancement.

#### Snapshot trigger surfaces

- **Declarative:** Create an `ImpVMSnapshot` resource. The controller reconciles it —
  triggers the node agent to pause the VM, dump memory + disk, push to the configured
  storage backend, then resumes the VM.
- **Imperative:** `kubectl imp snapshot <vm-name> --name <snapshot-name>` — kubectl
  plugin that creates the `ImpVMSnapshot` resource and streams status.

#### Snapshot distribution

| Backend | Description |
|---|---|
| `node-local` | Snapshot stays on the creating node. Scheduler prefers that node for warm VMs (no pull required). |
| `oci-registry` | Snapshot pushed as OCI artifact. Node agent pre-pulls to local storage on target nodes. |

**Recommended distribution tool:** [Spegel](https://github.com/spegel-org/spegel) —
a stateless P2P OCI registry mirror (Swedish: "mirror") deployed as a DaemonSet.
Nodes that already have a snapshot layer serve it directly to peers via Kademlia DHT,
eliminating registry round-trips. Strongly recommended in the Imp quickstart guide and
documented as the standard cluster setup. Not a hard requirement — any OCI registry
(GHCR, Harbor, ECR) or no registry (node-local only) works.

#### Warm pool spec

```go
type ImpWarmPoolSpec struct {
    // SnapshotRef names the ImpVMSnapshot to boot from.
    SnapshotRef string `json:"snapshotRef"`
    // Size is the number of pre-booted VMs to maintain.
    // +kubebuilder:default=2
    Size int32 `json:"size,omitempty"`
    // TemplateName is the ImpVMTemplate used to create pool members.
    TemplateName string `json:"templateName"`
}
```

The operator reconciles pool size continuously — replaces consumed VMs immediately.
Warm is the default when a matching snapshot exists; cold is the fallback.

#### Node agent pre-pull

The node agent (DaemonSet) watches for new `ImpVMSnapshot` resources with OCI backends
and proactively pulls the artifact to node-local storage. Pre-pull happens before any VM
is scheduled — ensuring warm start latency is always near zero.

---

## 4. VM Migration

### Problem

Node maintenance, pressure, or failure may require moving a running VM to another node
without losing its in-memory state.

### Constraint

Firecracker snapshot/restore requires the destination node to have a **compatible CPU
model**. Migration to a mismatched CPU yields undefined behaviour or boot failure.

### Design

#### CPU model detection

The node agent probes the host CPU model at startup (via `/proc/cpuinfo` or `cpuid`)
and stores it as a new field on `ClusterImpNodeProfile`:

```go
// CPUModel is the CPU model string detected by the node agent (e.g. "Intel(R) Core(TM) i5-8500T").
// +optional
CPUModel string `json:"cpuModel,omitempty"`
```

The migration scheduler filters candidate nodes by matching `cpuModel` against the
source node's value. Mismatched nodes are excluded silently (logged at debug level).

#### Migration CRD

```go
type ImpVMMigration struct {
    Spec   ImpVMMigrationSpec   `json:"spec"`
    Status ImpVMMigrationStatus `json:"status"`
}

type ImpVMMigrationSpec struct {
    // SourceVM is the namespace/name of the VM to migrate.
    SourceVM types.NamespacedName `json:"sourceVM"`

    // TargetNode optionally pins the destination. If empty, the scheduler picks
    // the best-fit compatible node.
    // +optional
    TargetNode string `json:"targetNode,omitempty"`
}

type ImpVMMigrationStatus struct {
    Phase   string       `json:"phase,omitempty"` // Pending, Running, Succeeded, Failed
    Message string       `json:"message,omitempty"`
    // +optional
    CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}
```

#### Migration trigger surfaces

- **Declarative:** Create an `ImpVMMigration` resource. Controller orchestrates:
  pause source VM → snapshot memory+disk → schedule on compatible destination → restore →
  delete source.
- **Imperative:** `kubectl imp migrate <vm-name> [--to <node-name>]` — creates the
  `ImpVMMigration` resource and tails status.

#### Automatic migration triggers

The controller watches for:

- **Node drain** (`node.kubernetes.io/unschedulable` taint) — migrates all VMs off the
  node automatically. Creates `ImpVMMigration` resources per VM.
- **Node pressure** (configurable threshold, e.g. memory > 90%) — migrates lower-priority
  VMs if a better-fit node exists.

Automatic migration creates `ImpVMMigration` resources using the same path as manual
migration — no separate code path.

#### Migration failure handling

If no compatible node exists: migration status set to `Failed` with reason
`NoCPUCompatibleNode`. Controller emits a Warning event on the ImpVM. VM remains
running on the source node.

---

## Cross-cutting notes

- **kubectl plugin** (`kubectl imp`) delivers `snapshot` and `migrate` subcommands.
  Implemented as a standalone Go binary distributed alongside the operator.
- **All new CRDs** (`ImpVMSnapshot`, `ImpVMMigration`, `ImpWarmPool`) are v1alpha1.
- **CiliumExternalWorkload** deferred — see §2 Networking.
- **Velero plugin** deferred — see §3 Warm VM Pools.
