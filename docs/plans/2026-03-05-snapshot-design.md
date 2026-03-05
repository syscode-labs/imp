# Imp — ImpVMSnapshot Design

> *Full snapshot lifecycle: declarative triggers, cron scheduling, OCI distribution, base image election*

**Goal:** Implement the `ImpVMSnapshot` controller end-to-end — agent-side Firecracker
snapshot capture, operator-side scheduling and retention, OCI/node-local storage, and
base image election for warm pools and migration.

---

## Overview

An `ImpVMSnapshot` captures the complete in-memory and on-disk state of a running
Firecracker VM. Snapshots serve three purposes:

1. **Base images** for `ImpWarmPool` — near-instant VM boot from a known state.
2. **Migration checkpoints** — restore a VM on a different node.
3. **Point-in-time backups** — manual or scheduled.

---

## Architecture

**Agent watches `ImpVMSnapshot` child objects directly.** The agent reconciler gains a
second controller that filters to snapshot executions where the source VM is scheduled on
its node. This is the same pattern as the existing `ImpVMReconciler` and requires no new
operator→agent RPC channel.

---

## CRD Design

### Updated `SnapshotStorageSpec`

```go
type SnapshotStorageSpec struct {
    // +kubebuilder:validation:Enum=node-local;oci-registry
    Type        string           `json:"type"`
    NodeLocal   *NodeLocalSpec   `json:"nodeLocal,omitempty"`
    OCIRegistry *OCIRegistrySpec `json:"ociRegistry,omitempty"`
}

type NodeLocalSpec struct {
    // Path is the base directory for snapshot artifacts on the node.
    // Can point to a remotely-mounted path (NFS, etc.).
    // +kubebuilder:default="/var/lib/imp/snapshots"
    Path string `json:"path,omitempty"`
}
```

### Updated `ImpVMSnapshotSpec`

```go
type ImpVMSnapshotSpec struct {
    SourceVMName      string               `json:"sourceVMName"`
    SourceVMNamespace string               `json:"sourceVMNamespace"`

    // Schedule is an optional cron expression for recurring snapshots.
    // +optional
    Schedule string `json:"schedule,omitempty"`

    // Retention is the max number of completed executions to keep.
    // +kubebuilder:default=3
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=10
    Retention int32 `json:"retention,omitempty"`

    Storage SnapshotStorageSpec `json:"storage"`

    // BaseSnapshot pins a specific child execution as the elected base image.
    // Set declaratively or via `kubectl imp elect`.
    // Consumers (ImpWarmPool, ImpVMMigration) use this as their boot source.
    // +optional
    BaseSnapshot string `json:"baseSnapshot,omitempty"`
}
```

### Updated `ImpVMSnapshotStatus`

```go
type ImpVMSnapshotStatus struct {
    Phase           string       `json:"phase,omitempty"`
    Digest          string       `json:"digest,omitempty"`
    SnapshotPath    string       `json:"snapshotPath,omitempty"`
    CompletedAt     *metav1.Time `json:"completedAt,omitempty"`
    NextScheduledAt *metav1.Time `json:"nextScheduledAt,omitempty"`

    // LastExecutionRef is the most recently created child execution.
    // +optional
    LastExecutionRef *corev1.LocalObjectReference `json:"lastExecutionRef,omitempty"`

    // BaseSnapshot mirrors spec.baseSnapshot once the referenced child is
    // validated as Succeeded. Consumers read this field only.
    // +optional
    BaseSnapshot string `json:"baseSnapshot,omitempty"`

    // +optional
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### Child execution objects

Each snapshot execution is itself an `ImpVMSnapshot` object owned by the parent, labelled
`imp.dev/snapshot-parent: <parent-name>`. Children carry a copy of the parent's spec and
individual status. They are never modified after creation except for status updates.

Child status gains one additional field:

```go
// TerminatedAt is set by the agent when the execution reaches a terminal state
// (Succeeded or Failed) and all cleanup is complete.
// The operator uses this — not phase alone — to gate new execution creation.
// +optional
TerminatedAt *metav1.Time `json:"terminatedAt,omitempty"`
```

---

## Snapshot Format

Firecracker produces two files per snapshot:

- **State file** — CPU registers, device state, VM metadata (~KB)
- **Memory file** — full RAM dump (~= VM memory size)

### `node-local` backend

Files written to `{spec.storage.nodeLocal.path}/{namespace}/{parent-name}/{child-name}/`:

```
/var/lib/imp/snapshots/default/my-snap/my-snap-20260305-0200/
  vm.state
  vm.mem
```

Path is configurable per snapshot — supports NFS, remote block storage, or any
POSIX-accessible mount.

### `oci-registry` backend

Files pushed as a two-layer OCI image:

| Layer | Content | Media type |
|---|---|---|
| Layer 1 | `vm.state` (gzipped) | `application/vnd.oci.image.layer.v1.tar+gzip` |
| Layer 2 | `vm.mem` (gzipped) | `application/vnd.oci.image.layer.v1.tar+gzip` |

Tag format: `{repository}:{namespace}-{parent-name}-{timestamp}`

Standard OCI tooling (crane, skopeo) works out of the box. **Spegel** (recommended
cluster setup) distributes layers P2P via Kademlia DHT — nodes that already have a layer
serve it directly to peers, eliminating registry round-trips. Unchanged state-file layers
are automatically deduplicated across snapshots of the same VM.

**Snapshot type:** Full only. Diff snapshots (changed pages only) are on the roadmap.

---

## Reconciliation Flow

### Operator-side `ImpVMSnapshotReconciler` (parent objects)

1. **One-shot** (no schedule): creates a single child execution object. Sets
   `status.lastExecutionRef`. Waits.
2. **Scheduled**: parses `spec.schedule`. On each cron tick:
   - Checks for any child without a `terminatedAt` — skips tick if found
     (`concurrencyPolicy: Forbid` semantics).
   - Creates new child (copy of parent spec + `ownerReference` +
     `imp.dev/snapshot-parent` label).
   - Updates `status.lastExecutionRef` and `status.nextScheduledAt`.
   - Prunes oldest children beyond `retention` (sorted by `creationTimestamp`,
     deletes from oldest).
3. **BaseSnapshot validation**: when `spec.baseSnapshot` is set, verifies the named
   child exists and has `phase=Succeeded`. If valid, mirrors to `status.baseSnapshot`.
   If invalid, sets a `BaseSnapshotInvalid` condition.

### Agent-side `ImpVMSnapshotReconciler` (child execution objects)

Filters: `spec.sourceVMNamespace/sourceVMName` must map to a VM with
`spec.nodeName == r.NodeName`.

1. Set child `status.phase = Running`.
2. Look up the source VM's `fcProc` in the driver.
3. Call `d.Driver.Snapshot(ctx, vm)`:
   - **Always** registers `defer d.Driver.ResumeVM(vm)` first — VM is never left paused.
   - Calls Firecracker `PauseVm`.
   - Calls Firecracker `CreateSnapshot` → returns `{StatePath, MemPath}`.
   - Resume happens via deferred call.
4. **`node-local`**: move files to configured path. Set `status.snapshotPath`.
5. **`oci-registry`**: push two-layer OCI image via `go-containerregistry`. Set
   `status.digest`.
6. On success: set `phase=Succeeded`, `completedAt=now`, `terminatedAt=now`.
7. On any error: ensure cleanup complete (temp files removed, VM confirmed running),
   then set `phase=Failed`, `status.message=<reason>`, `terminatedAt=now`.

The `terminatedAt` field is set only after all cleanup is complete — not at the moment
of failure. This is the serialisation gate: the operator will not create the next child
until `terminatedAt` is populated.

---

## Fault Tolerance

| Scenario | Handling |
|---|---|
| VM not Running | Child `Failed/VMNotReady`. Operator requeues 30 s. |
| Firecracker pause fails | Child `Failed/PauseFailed`. VM never paused. |
| `CreateSnapshot` fails after pause | `defer ResumeVM` fires. Child `Failed/SnapshotFailed`. |
| OCI push fails | Child `Failed/PushFailed`. State+memory files kept in temp dir. Operator creates new child on next reconcile (exponential backoff). Temp dir cleaned on retry start. |
| Agent crash mid-snapshot | Child stuck `Running`. After `snapshotTimeout` (default 5 m), operator sets child `Failed/Timeout`. |
| Node goes down | Same timeout applies. Operator detects stale `Running` child, creates replacement on next tick. |

**Core invariant:** `defer d.Driver.ResumeVM(vm)` is registered before the first
Firecracker API call. The VM resumes regardless of what fails after that point.

---

## Serialisation Guarantee

> **At most one active execution per parent at any time.**

The operator checks for any child missing `terminatedAt` before creating the next one.
A failed execution must reach terminal state (agent sets `terminatedAt` after cleanup)
before the next one fires. If the agent crashes before setting `terminatedAt`, the
operator's `snapshotTimeout` forces the child to `Failed/Timeout` with a synthetic
`terminatedAt`, unblocking the schedule.

---

## Base Image Election

A snapshot execution becomes a base image only by **explicit election** — it is never
automatic.

- **Imperative:** `kubectl imp elect <parent> [--execution <child-name>]`
  If `--execution` is omitted, the latest `Succeeded` child is elected.
- **Declarative:** patch `spec.baseSnapshot: <child-name>`.

`ImpWarmPool` reads `status.baseSnapshot`. If unset, the pool stays idle — there is no
implicit fallback to latest. This prevents accidental rollout of an unvalidated snapshot
to production CI runners.

Elected children are exempt from retention pruning. The operator never deletes a child
referenced by `status.baseSnapshot`.

---

## ImpWarmPool Integration

`ImpWarmPool.Spec.SnapshotRef` names an `ImpVMSnapshot` parent. The warm pool controller
resolves the boot artifact as follows:

1. If `status.baseSnapshot` is set on the parent → use that child's artifact.
2. Otherwise → pool stays idle, emits `BaseSnapshotNotElected` event.

This means warm pools require an explicit election before they boot any VMs.

---

## Trigger Surfaces

| Surface | Mechanism |
|---|---|
| Declarative (one-shot) | Create `ImpVMSnapshot` resource |
| Declarative (scheduled) | Create `ImpVMSnapshot` with `spec.schedule` |
| Imperative (one-shot) | `kubectl imp snapshot <vm>` — creates resource, streams status |
| Imperative (elect) | `kubectl imp elect <parent> [--execution <child>]` |

---

## Deferred

- **Diff snapshots** — changed-pages-only memory dumps for space efficiency. Requires
  dependency-aware retention (cannot prune base of a diff chain).
- **Velero plugin** — direct integration for backup workflows.
- **Pre-pull on agent startup** — agent DaemonSet proactively pulls elected OCI artifacts
  to node-local storage ahead of warm pool scheduling.
