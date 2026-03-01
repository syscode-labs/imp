# ImpVM Controller Design

**Date:** 2026-03-01
**Scope:** Operator reconcile loop — scheduler, status sync, node health, deletion, probes

---

## 1. Overview

The `ImpVMReconciler` manages the full lifecycle of an `ImpVM` object. It operates
at the operator layer (Layer 2) and never calls Firecracker directly. The node agent
(Layer 1) owns Firecracker processes; the operator owns object state.

**Key principle:** operator and agent write to non-overlapping status fields.

| Field owner | Fields |
|---|---|
| Operator | `status.conditions` |
| Agent | `status.phase`, `status.ip`, `status.runtimePID`, `status.nodeName` |

---

## 2. Reconcile Loop

```
Reconcile(ImpVM):
  1. Fetch ImpVM — if not found, return (deleted)
  2. Handle deletion (finalizer)
  3. If spec.nodeName == "" → Schedule
  4. If spec.nodeName != "" → SyncStatus
```

A single finalizer `imp/finalizer` is added on creation and removed only after
the agent confirms the VM has stopped.

---

## 3. Scheduler

Runs when `spec.nodeName == ""`.

### 3.1 Node Eligibility

Only nodes with the label `imp/enabled=true` are considered. This is the
community-standard opt-in pattern (mirrors GPU device plugin, Kata Containers, etc.).

Additional filtering via `spec.nodeSelector` (same semantics as
`Pod.spec.nodeSelector`).

### 3.2 Capacity

Each node's capacity is determined by its `ClusterImpNodeProfile` (matched by node
name, 1:1). If no profile exists for a node, the node has no cap.

Running VM count = ImpVMs where `spec.nodeName == node AND phase != Failed`.

### 3.3 Algorithm

```
Schedule(ImpVM):
  1. List nodes: label imp/enabled=true
  2. Filter by spec.nodeSelector
  3. For each eligible node:
       a. Fetch ClusterImpNodeProfile (if exists)
       b. maxImpVMs = profile.spec.maxImpVMs (skip if count >= max)
       c. Count running ImpVMs on node
  4. Pick node with fewest running ImpVMs
     Tie-break: alphabetical by node name (deterministic)
  5. No node found:
       - Set phase=Pending
       - Emit Warning event: reason=Unschedulable
       - Requeue after 30s
  6. Node found:
       - Set spec.nodeName
       - Set Scheduled=True condition
       - Set phase=Scheduled
```

---

## 4. Status Sync

Runs when `spec.nodeName != ""`.

### 4.1 Node Health Check

```
SyncStatus(ImpVM):
  1. Fetch assigned node
  2. If node not found OR node Ready condition = False/Unknown:
       - Set NodeHealthy=False condition
       - If lifecycle=ephemeral:
           clear spec.nodeName, set phase=Pending
           emit Normal event: reason=Rescheduling
           requeue immediately
       - If lifecycle=persistent:
           set phase=Failed
           emit Warning event: reason=NodeLost
       - Return
  3. Set NodeHealthy=True condition
  4. Set Scheduled=True (always true at this point)
  5. Derive Ready condition from status.phase:
       Running   → Ready=True
       Pending/Scheduled → Ready=Unknown
       Failed/Succeeded  → Ready=False
  6. Patch status.conditions
```

### 4.2 Conditions

| Condition | Tracks | Values |
|---|---|---|
| `Scheduled` | VM — has a node been assigned | True / False |
| `Ready` | VM — is the VM running and healthy | True / False / Unknown |
| `NodeHealthy` | Node — is the assigned node Ready | True / False |

### 4.3 Requeue Strategy

| Situation | Requeue |
|---|---|
| No node available | 30s |
| Ready=Unknown (waiting for agent) | 10s |
| Waiting for agent to terminate VM | 5s |
| Steady state (Running, healthy) | none — watch-driven |

In steady state the controller only reconciles on watch events (agent writing
status fields, node condition changes). No polling.

---

## 5. Deletion and Finalizer

```
HandleDeletion(ImpVM):
  1. If finalizer absent → return
  2. If spec.nodeName != "":
       - Set phase=Terminating
       - Requeue every 5s waiting for agent to stop VM
       - Agent: watches phase=Terminating, stops Firecracker,
                clears status.nodeName, sets phase=Succeeded
       - Timeout 2min → force-remove finalizer
                        emit Warning: reason=TerminationTimeout
  3. If spec.nodeName == "" (unscheduled or agent cleaned up):
       - Remove finalizer → object deleted
```

Force-removal after timeout prevents stuck deletions when a node is permanently
lost. The Warning event informs the user that cleanup may be incomplete.

---

## 6. Health Checks

Two probe layers, complementary:

### 6.1 Agent Probes (always on)

The agent runs `startupProbe`, `readinessProbe`, and `livenessProbe` from
`spec.probes` via VSOCK (host↔VM, no network overhead). Results are written to
`status.phase`. The operator reads phase and derives the `Ready` condition from it.

No probe logic in the operator for this path.

### 6.2 Operator HTTP Probe (opt-in)

The operator can directly HTTP-check `status.ip` for VMs that expose a health
endpoint. Enabled per-VM or globally via `ClusterImpConfig`.

**Configuration (layered — ClusterImpConfig default, ImpVM override):**

```yaml
# ClusterImpConfig (global default)
spec:
  defaultHttpCheck:
    enabled: false

# ImpVM (per-instance override)
spec:
  probes:
    httpCheck:
      enabled: true
      path: /healthz
      port: 8080
      intervalSeconds: 10
      failureThreshold: 3
```

**Behaviour:**

- Operator polls `http://status.ip:port/path` every `intervalSeconds`
- `failureThreshold` consecutive failures → `Ready=False` + Warning event
- Recovery (probe passes again) → `Ready=True` + Normal event

Requires network reachability from the operator pod to VM IPs. Not suitable for
all network configurations — hence opt-in.

---

## 7. Events

All events are emitted on the `ImpVM` object.

| Reason | Type | When |
|---|---|---|
| `Scheduled` | Normal | VM assigned to a node |
| `Unschedulable` | Warning | No eligible node with available capacity |
| `NodeLost` | Warning | Assigned node went NotReady, persistent VM failed |
| `Rescheduling` | Normal | Ephemeral VM cleared and requeued after node loss |
| `Terminating` | Normal | Deletion started, waiting for agent |
| `TerminationTimeout` | Warning | Finalizer force-removed after 2min timeout |
| `HealthCheckFailed` | Warning | HTTP probe failed failureThreshold times |
| `HealthCheckRecovered` | Normal | HTTP probe passing again after failure |

---

## 8. API Changes Required

Before implementing the controller, the following changes are needed:

### 8.1 Label prefix: imp.dev → imp

All labels and annotations defined by this project use `imp/` prefix (not
`imp.dev/` — domain not yet registered).

- Node eligibility label: `imp/enabled: "true"`
- Finalizer: `imp/finalizer`
- Update all references in `config/manager/manager.yaml`, `AGENTS.md`, docs

The CRD API group (`imp.dev/v1alpha1`) is established in the Go types and CRD
manifests — changing it requires re-scaffolding and is deferred.

### 8.2 New fields on ImpVMSpec

```go
// spec.probes.httpCheck
type HTTPCheckSpec struct {
    Enabled          bool   `json:"enabled,omitempty"`
    Path             string `json:"path,omitempty"`
    Port             int32  `json:"port,omitempty"`
    IntervalSeconds  int32  `json:"intervalSeconds,omitempty"`
    FailureThreshold int32  `json:"failureThreshold,omitempty"`
}
```

Add `HTTPCheck *HTTPCheckSpec` to `ProbeSpec` in `api/v1alpha1/shared_types.go`.

### 8.3 New field on ClusterImpConfigSpec

```go
// spec.defaultHttpCheck
DefaultHttpCheck *HTTPCheckSpec `json:"defaultHttpCheck,omitempty"`
```

Add to `ClusterImpConfigSpec` in `api/v1alpha1/clusterimpconfig_types.go`.

### 8.4 status.runtimePID

Rename `firecrackerPID` → `runtimePID` in `ImpVMStatus` for provider neutrality.

---

## 9. Packages

```
internal/controller/
    impvm_controller.go      ← reconcile loop (scheduler + status sync + deletion)
    impvm_scheduler.go       ← node selection logic
    impvm_health.go          ← HTTP probe runner
    impvm_conditions.go      ← condition helpers
    events.go                ← event reason constants
```

No business logic outside `internal/controller/` for the operator layer. The
agent's business logic lives in `internal/agent/`.
