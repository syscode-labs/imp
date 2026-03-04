# Phase 2 Deferred Items Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement three deferred Phase 1 items: NAT teardown reference counting, probe condition patching, and resource-aware VM scheduling with autoscaler-friendly signalling.

**Architecture:** All three are self-contained changes. NAT teardown extends the in-memory Allocator. Probe patching wires the existing no-op patcher to a direct status patch via d.Client. Scheduling adds a pure-function scheduler and two new ClusterImpNodeProfile capacity fields.

**Tech Stack:** Go, controller-runtime, k8s.io/apimachinery/pkg/api/meta, nft/iptables

---

## 1. NAT Teardown Reference Counting

### What

Extend `network.Allocator` with a per-network VM count. When the last VM releases its IP, `FirecrackerDriver.Stop` removes the NAT rule for that subnet.

### Design

**`internal/agent/network/allocator.go`**
- Add `vmCount map[string]int` to `Allocator`
- `Allocate` increments `vmCount[netKey]`
- `Release(netKey, ip string) (wasLast bool)` decrements; returns true when count hits 0

**`internal/agent/network/net.go`**
- Add `RemoveNAT(ctx, subnet, egressInterface string) error` to `NetManager` interface
- `LinuxNetManager.RemoveNAT` deletes the nft masquerade rule for the subnet (idempotent)
- `StubNetManager.RemoveNAT` returns nil

**`internal/agent/firecracker_driver.go`**
- In `Stop()`, after `d.Alloc.Release(...)`, check `wasLast`
- If true and NAT was enabled: call `d.Net.RemoveNAT(ctx, proc.netInfo.Subnet, proc.netInfo.EgressInterface)`
- Store `Subnet` and `EgressInterface` on `NetworkInfo` (already has `Subnet`; add `EgressInterface`)

**Restart behaviour:** Stale NAT rules on agent restart are acceptable — `EnsureNAT` is idempotent and re-installs on the next VM start.

---

## 2. Probe Condition Patching

### What

Wire the no-op patcher in `runProbes` to directly patch `ImpVM.status.conditions` via `d.Client`.

### Design

**`api/v1alpha1/impvm_types.go`**
- Add to `ImpVMStatus`:
```go
// +listType=map
// +listMapKey=type
// +optional
Conditions []metav1.Condition `json:"conditions,omitempty"`
```
- Run `make generate` to update deepcopy

**`internal/agent/firecracker_driver.go`**
- Capture `vmNamespace`, `vmName` before goroutine launch (alongside `probes`)
- Pass to `runProbes(ctx, probes, vsockPath, vmNamespace, vmName)`
- Patcher function:
```go
patcher := func(conds []metav1.Condition) {
    vm := &impv1alpha1.ImpVM{}
    nn := types.NamespacedName{Namespace: vmNamespace, Name: vmName}
    if err := d.Client.Get(ctx, nn, vm); err != nil {
        logf.Log.Error(err, "probe patcher: Get failed", "vm", nn)
        return
    }
    base := vm.DeepCopy()
    for _, c := range conds {
        apimeta.SetStatusCondition(&vm.Status.Conditions, c)
    }
    if err := d.Client.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
        logf.Log.Error(err, "probe patcher: Patch failed", "vm", nn)
    }
}
```
- Import `apimeta "k8s.io/apimachinery/pkg/api/meta"`

**Condition types:** `Started` (StartupProbe), `Ready` (ReadinessProbe), `Healthy` (LivenessProbe) — matching the probe runner's existing condition type strings.

---

## 3. Resource-Aware VM Scheduling

### What

Replace the naive node picker with resource-fitting. When no node fits, mark the VM `Unschedulable` and emit an Event. Autoscaler-agnostic.

### Design

**`api/v1alpha1/clusterimpnodeprofile_types.go`**
- Add to `ClusterImpNodeProfileSpec`:
```go
// VCPUCapacity is the total number of vCPUs available for VMs on this node.
// +optional
VCPUCapacity int32 `json:"vcpuCapacity,omitempty"`
// MemoryMiB is the total memory in MiB available for VMs on this node.
// +optional
MemoryMiB int64 `json:"memoryMiB,omitempty"`
```
- Run `make generate`

**`internal/controller/scheduler.go`** (new file)
```go
// NodeInfo holds capacity and current load for a candidate node.
type NodeInfo struct {
    NodeName     string
    VCPUCapacity int32
    MemoryMiB    int64
    UsedVCPU     int32
    UsedMemoryMiB int64
}

// Schedule picks the best-fit node for the given VM class requirements.
// Returns ("", ErrUnschedulable) when no node has sufficient capacity.
// Logs debug information for each candidate evaluated.
func Schedule(log logr.Logger, vcpu int32, memMiB int64, nodes []NodeInfo) (string, error)
```

Selection algorithm:
1. Filter nodes where `freeVCPU >= vcpu && freeMemMiB >= memMiB`
2. Log each candidate: node name, free VCPU, free memory, fit result
3. Tie-break: highest free memory (bin-packing)
4. Return `ErrUnschedulable` (sentinel error) if no candidates pass

Debug log fields per candidate: `node`, `freeVCPU`, `freeMemMiB`, `required.vcpu`, `required.memMiB`, `fits`.

**`internal/controller/impvm_controller.go`**

In the scheduling path (`handlePending` or equivalent):
1. List agent pods to find candidate nodes
2. For each candidate, fetch `ClusterImpNodeProfile` by node name
3. List running/scheduled/starting ImpVMs per node; sum class resources
4. Call `scheduler.Schedule(log, class.Spec.VCPU, class.Spec.MemoryMiB, nodes)`
5. On success: assign `vm.Spec.NodeName = nodeName`
6. On `ErrUnschedulable`:
   - Set `status.phase = Pending` (keep)
   - `meta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{Type: "Unschedulable", Status: metav1.ConditionTrue, Reason: "InsufficientCapacity", Message: "no node has sufficient vcpu/memory"})`
   - Emit Event: `r.Recorder.Event(vm, corev1.EventTypeWarning, "Unschedulable", "no node has sufficient capacity")`
   - `ctrl.Result{RequeueAfter: 30 * time.Second}`

**`internal/controller/scheduler_test.go`** (new file)
- Pure unit tests, no envtest needed
- Table-driven: fits single node, picks best-fit, returns unschedulable, tie-breaks by memory
