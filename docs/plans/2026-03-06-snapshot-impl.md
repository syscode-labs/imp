# ImpVMSnapshot Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Implement end-to-end ImpVMSnapshot — Firecracker pause/snapshot/resume on the agent, node-local and OCI-registry storage, operator-side cron scheduling with parent/child execution model, retention pruning, and base snapshot election.

**Architecture:** Two reconcilers: an operator-side reconciler manages parent objects (creates child executions, enforces retention, validates BaseSnapshot election); an agent-side reconciler handles child executions on the node where the source VM runs (calls FirecrackerDriver.Snapshot, stores artifact, sets TerminatedAt). The VMDriver interface gains a `Snapshot` method. No new RPC channel — the agent watches ImpVMSnapshot children filtered by node.

**Tech Stack:** Go 1.25, controller-runtime v0.20, `github.com/google/go-containerregistry` (already in go.mod), `github.com/robfig/cron/v3` (new dep for cron parsing), Firecracker Go SDK (`PauseVm`, `CreateSnapshot`, `ResumeVm`).

---

## Context

- Repo: `/Users/giovanni/syscode/git/imp`
- Design doc: `docs/plans/2026-03-05-snapshot-design.md`
- Run tests: `go test ./...` (controller suite requires kubebuilder binaries, skip with `-run ''` or use package-specific runs)
- Build check: `GOOS=linux go build ./...`
- All commits: no "Co-Authored-By: Claude" lines; no AI mention

---

## Task 1: `Snapshot` on VMDriver interface + StubDriver

Add `Snapshot(ctx, vm, destDir) (SnapshotResult, error)` to the `VMDriver` interface and implement it on `StubDriver`. `FirecrackerDriver` is handled in Task 2.

**Files:**
- Modify: `internal/agent/driver.go`
- Modify: `internal/agent/stub_driver.go`
- Modify: `internal/agent/stub_driver_test.go` (or create if absent)

**Step 1: Write the failing test**

In `internal/agent/stub_driver_test.go`, add:

```go
func TestStubDriver_Snapshot_success(t *testing.T) {
    d := NewStubDriver()
    vm := &impdevv1alpha1.ImpVM{}
    vm.Namespace, vm.Name = "ns", "vm1"
    // Start first so the VM is "running"
    _, _ = d.Start(context.Background(), vm)

    res, err := d.Snapshot(context.Background(), vm, t.TempDir())
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if res.StatePath == "" || res.MemPath == "" {
        t.Errorf("expected non-empty paths, got %+v", res)
    }
}

func TestStubDriver_Snapshot_notRunning(t *testing.T) {
    d := NewStubDriver()
    vm := &impdevv1alpha1.ImpVM{}
    vm.Namespace, vm.Name = "ns", "vm-missing"

    _, err := d.Snapshot(context.Background(), vm, t.TempDir())
    if err == nil {
        t.Error("expected error for non-running VM")
    }
}
```

**Step 2: Run to verify failure**

```bash
cd /Users/giovanni/syscode/git/imp
go test ./internal/agent/ -run TestStubDriver_Snapshot -v
```

Expected: compile error — `Snapshot` not defined on interface.

**Step 3: Add `SnapshotResult` and `Snapshot` to `driver.go`**

```go
// SnapshotResult holds the paths of the two files produced by a Firecracker snapshot.
type SnapshotResult struct {
    // StatePath is the path to the VM state file (CPU registers, device state).
    StatePath string
    // MemPath is the path to the memory dump file.
    MemPath string
}

// In VMDriver interface, add after Inspect:

// Snapshot pauses the VM, writes state+memory files to destDir, then resumes it.
// The VM is always resumed before Snapshot returns, even on error.
// destDir must exist and be writable.
Snapshot(ctx context.Context, vm *impdevv1alpha1.ImpVM, destDir string) (SnapshotResult, error)
```

**Step 4: Implement on `StubDriver`**

Add to `stub_driver.go`:

```go
// Snapshot writes empty placeholder files to destDir and returns their paths.
// Returns an error if the VM is not currently running.
func (d *StubDriver) Snapshot(_ context.Context, vm *impdevv1alpha1.ImpVM, destDir string) (SnapshotResult, error) {
    d.mu.Lock()
    _, ok := d.states[vmKey(vm)]
    d.mu.Unlock()
    if !ok {
        return SnapshotResult{}, fmt.Errorf("VM %s/%s is not running", vm.Namespace, vm.Name)
    }
    statePath := filepath.Join(destDir, "vm.state")
    memPath := filepath.Join(destDir, "vm.mem")
    if err := os.WriteFile(statePath, []byte("stub-state"), 0o600); err != nil {
        return SnapshotResult{}, err
    }
    if err := os.WriteFile(memPath, []byte("stub-mem"), 0o600); err != nil {
        return SnapshotResult{}, err
    }
    return SnapshotResult{StatePath: statePath, MemPath: memPath}, nil
}
```

Add imports to `stub_driver.go`: `"os"`, `"path/filepath"`.

**Step 5: Run tests**

```bash
go test ./internal/agent/ -run TestStubDriver_Snapshot -v
```

Expected: both tests PASS.

**Step 6: Build check**

```bash
GOOS=linux go build ./...
```

Expected: compile error — `FirecrackerDriver` does not implement `VMDriver` (missing `Snapshot`). This is expected; Task 2 fixes it.

**Step 7: Add temporary stub on FirecrackerDriver to unblock compilation**

In `internal/agent/firecracker_driver.go`, add at the bottom (before the compile-time check):

```go
// Snapshot implements VMDriver. Full implementation in Task 2.
func (d *FirecrackerDriver) Snapshot(_ context.Context, vm *impdevv1alpha1.ImpVM, _ string) (SnapshotResult, error) {
    return SnapshotResult{}, fmt.Errorf("snapshot not yet implemented for %s/%s", vm.Namespace, vm.Name)
}
```

**Step 8: Build check passes**

```bash
GOOS=linux go build ./...
```

Expected: no errors.

**Step 9: Commit**

```bash
git add internal/agent/driver.go internal/agent/stub_driver.go internal/agent/stub_driver_test.go internal/agent/firecracker_driver.go
git commit -m "feat(agent): add Snapshot to VMDriver interface, stub implementation"
```

---

## Task 2: `FirecrackerDriver.Snapshot` — pause/snapshot/resume + node-local storage

Replace the stub with the real Firecracker implementation. Uses `machine.PauseVM`, `machine.CreateSnapshot`, `machine.ResumeVM` from the Firecracker Go SDK.

**Files:**
- Modify: `internal/agent/firecracker_driver.go`
- Modify: `internal/agent/firecracker_driver_test.go`

**Step 1: Write the failing test**

The FirecrackerDriver uses a real Firecracker process, so we test the error path (no running VM) and the defer-resume guarantee. Add to `firecracker_driver_test.go`:

```go
func TestFirecrackerDriver_Snapshot_noVM_returnsError(t *testing.T) {
    d := &FirecrackerDriver{procs: make(map[string]*fcProc)}
    vm := &impdevv1alpha1.ImpVM{}
    vm.Namespace, vm.Name = "ns", "missing"

    _, err := d.Snapshot(context.Background(), vm, t.TempDir())
    if err == nil {
        t.Error("expected error for non-running VM")
    }
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/agent/ -run TestFirecrackerDriver_Snapshot_noVM -v
```

Expected: FAIL — stub returns "not yet implemented", not "not running".

**Step 3: Replace stub with real implementation in `firecracker_driver.go`**

Replace the stub `Snapshot` method with:

```go
// Snapshot pauses the VM, writes state+memory files to destDir, then resumes it.
// The VM is always resumed before returning, even on error (enforced via defer).
// destDir must exist; files are named vm.state and vm.mem.
func (d *FirecrackerDriver) Snapshot(ctx context.Context, vm *impdevv1alpha1.ImpVM, destDir string) (SnapshotResult, error) {
    key := vmKey(vm)
    d.mu.Lock()
    proc, ok := d.procs[key]
    d.mu.Unlock()
    if !ok {
        return SnapshotResult{}, fmt.Errorf("VM %s is not running on this node", key)
    }

    log := logf.FromContext(ctx).WithValues("vm", key)

    // Pause the VM. Always resume via defer — VM must never be left paused.
    if err := proc.machine.PauseVM(ctx); err != nil {
        return SnapshotResult{}, fmt.Errorf("pause VM: %w", err)
    }
    defer func() {
        if err := proc.machine.ResumeVM(ctx); err != nil {
            log.Error(err, "ResumeVM failed after snapshot — VM may be paused")
        }
    }()

    statePath := filepath.Join(destDir, "vm.state")
    memPath := filepath.Join(destDir, "vm.mem")

    snapshotType := models.SnapshotTypeFull
    if err := proc.machine.CreateSnapshot(ctx, memPath, statePath, firecracker.WithSnapshotType(snapshotType)); err != nil {
        return SnapshotResult{}, fmt.Errorf("CreateSnapshot: %w", err)
    }

    log.Info("snapshot captured", "statePath", statePath, "memPath", memPath)
    return SnapshotResult{StatePath: statePath, MemPath: memPath}, nil
}
```

Add imports as needed:
- `"path/filepath"`
- `"github.com/firecracker-microvm/firecracker-go-sdk"` (already imported as `firecracker`)
- `"github.com/firecracker-microvm/firecracker-go-sdk/client/models"`

**Step 4: Run tests**

```bash
go test ./internal/agent/ -run TestFirecrackerDriver_Snapshot -v
```

Expected: `TestFirecrackerDriver_Snapshot_noVM_returnsError` PASS.

**Step 5: Build check**

```bash
GOOS=linux go build ./...
```

Expected: no errors.

**Step 6: Commit**

```bash
git add internal/agent/firecracker_driver.go internal/agent/firecracker_driver_test.go
git commit -m "feat(agent): FirecrackerDriver.Snapshot — pause/CreateSnapshot/defer-resume"
```

---

## Task 3: OCI push helper

Create `internal/agent/snapshot/oci.go` — pushes the two snapshot files as a two-layer OCI image using `go-containerregistry`. State file is layer 1, memory file is layer 2.

**Files:**
- Create: `internal/agent/snapshot/oci.go`
- Create: `internal/agent/snapshot/oci_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/snapshot/oci_test.go
package snapshot_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/google/go-containerregistry/pkg/registry"
    "github.com/syscode-labs/imp/internal/agent/snapshot"
)

func TestPushOCI_roundTrip(t *testing.T) {
    // Start an in-memory registry.
    reg := registry.New()
    srv := httptest.NewServer(reg)
    t.Cleanup(srv.Close)

    dir := t.TempDir()
    statePath := filepath.Join(dir, "vm.state")
    memPath := filepath.Join(dir, "vm.mem")
    _ = os.WriteFile(statePath, []byte("state-data"), 0o600)
    _ = os.WriteFile(memPath, []byte("mem-data"), 0o600)

    repo := strings.TrimPrefix(srv.URL, "http://") + "/test/snap"
    digest, err := snapshot.PushOCI(context.Background(), statePath, memPath, repo, "latest", nil)
    if err != nil {
        t.Fatalf("PushOCI: %v", err)
    }
    if digest == "" {
        t.Error("expected non-empty digest")
    }
}
```

Add missing imports: `"context"`, `"net/http/httptest"`, `"strings"`.

**Step 2: Run to verify failure**

```bash
go test ./internal/agent/snapshot/ -run TestPushOCI -v
```

Expected: compile error — package does not exist.

**Step 3: Create `internal/agent/snapshot/oci.go`**

```go
// Package snapshot provides helpers for pushing and pulling Firecracker snapshot
// artifacts as two-layer OCI images.
package snapshot

import (
    "context"
    "fmt"

    "github.com/google/go-containerregistry/pkg/authn"
    "github.com/google/go-containerregistry/pkg/name"
    v1 "github.com/google/go-containerregistry/pkg/v1"
    "github.com/google/go-containerregistry/pkg/v1/empty"
    "github.com/google/go-containerregistry/pkg/v1/mutate"
    "github.com/google/go-containerregistry/pkg/v1/remote"
    "github.com/google/go-containerregistry/pkg/v1/tarball"
    corev1 "k8s.io/api/core/v1"
)

// PushOCI packages statePath (layer 1) and memPath (layer 2) as a two-layer OCI
// image and pushes it to repository:tag. pullSecretRef is unused in Phase 1
// (anonymous auth); it is accepted for future registry credential support.
// Returns the manifest digest (e.g. "sha256:abc123...").
func PushOCI(ctx context.Context, statePath, memPath, repository, tag string, _ *corev1.LocalObjectReference) (string, error) {
    ref, err := name.ParseReference(fmt.Sprintf("%s:%s", repository, tag))
    if err != nil {
        return "", fmt.Errorf("parse reference: %w", err)
    }

    stateLayer, err := tarball.LayerFromFile(statePath)
    if err != nil {
        return "", fmt.Errorf("state layer: %w", err)
    }
    memLayer, err := tarball.LayerFromFile(memPath)
    if err != nil {
        return "", fmt.Errorf("mem layer: %w", err)
    }

    img, err := mutate.AppendLayers(empty.Image, stateLayer, memLayer)
    if err != nil {
        return "", fmt.Errorf("append layers: %w", err)
    }

    if err := remote.Write(ref, img, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
        return "", fmt.Errorf("push image: %w", err)
    }

    digest, err := img.Digest()
    if err != nil {
        return "", fmt.Errorf("get digest: %w", err)
    }
    return digest.String(), nil
}

// DigestForRef returns the manifest digest of an existing OCI image at ref.
// Used to populate status.digest after a successful push.
func DigestForRef(ref v1.Hash) string {
    return ref.String()
}
```

**Step 4: Run tests**

```bash
go test ./internal/agent/snapshot/ -run TestPushOCI -v
```

Expected: PASS.

**Step 5: Build check**

```bash
GOOS=linux go build ./...
```

Expected: no errors.

**Step 6: Commit**

```bash
git add internal/agent/snapshot/oci.go internal/agent/snapshot/oci_test.go
git commit -m "feat(agent): OCI push helper — two-layer snapshot image via go-containerregistry"
```

---

## Task 4: Agent-side `ImpVMSnapshotReconciler`

Add a second reconciler to the agent that handles child `ImpVMSnapshot` execution objects. Filters to children where the source VM is on this node.

**Files:**
- Create: `internal/agent/impvmsnapshot_reconciler.go` (build tag: `//go:build linux`)
- Create: `internal/agent/impvmsnapshot_reconciler_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/impvmsnapshot_reconciler_test.go
package agent_test

import (
    "context"
    "testing"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
    "github.com/syscode-labs/imp/internal/agent"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSnapshotReconciler_skipsWrongNode(t *testing.T) {
    vm := &impdevv1alpha1.ImpVM{}
    vm.Name, vm.Namespace = "my-vm", "default"
    vm.Spec.NodeName = "other-node"

    snap := &impdevv1alpha1.ImpVMSnapshot{}
    snap.Name, snap.Namespace = "snap-child", "default"
    snap.Spec.SourceVMName = "my-vm"
    snap.Spec.SourceVMNamespace = "default"
    snap.Spec.Storage = impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"}

    scheme := agent.TestScheme(t)
    c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vm, snap).WithStatusSubresource(snap).Build()

    r := &agent.ImpVMSnapshotReconciler{
        Client:   c,
        NodeName: "this-node",
        Driver:   agent.NewStubDriver(),
    }

    res, err := r.Reconcile(context.Background(), reconcile.Request{
        NamespacedName: types.NamespacedName{Name: "snap-child", Namespace: "default"},
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if res.Requeue || res.RequeueAfter != 0 {
        t.Errorf("expected no-op result for wrong node, got %+v", res)
    }
}
```

Note: `agent.TestScheme` is a helper that registers all imp types. Add it to a `testhelpers_test.go` or inline it. The fake client needs `impdevv1alpha1` registered — use `impdevv1alpha1.AddToScheme`.

**Step 2: Run to verify failure**

```bash
go test ./internal/agent/ -run TestSnapshotReconciler -v
```

Expected: compile error — `ImpVMSnapshotReconciler` not defined.

**Step 3: Create `internal/agent/impvmsnapshot_reconciler.go`**

```go
//go:build linux

package agent

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    logf "sigs.k8s.io/controller-runtime/pkg/log"

    impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
    "github.com/syscode-labs/imp/internal/agent/snapshot"
)

const snapshotTempDirPrefix = "imp-snapshot-"

// ImpVMSnapshotReconciler handles child ImpVMSnapshot execution objects on this node.
type ImpVMSnapshotReconciler struct {
    client.Client
    Scheme   *runtime.Scheme
    NodeName string
    Driver   VMDriver
}

func (r *ImpVMSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := logf.FromContext(ctx)

    snap := &impdevv1alpha1.ImpVMSnapshot{}
    if err := r.Get(ctx, req.NamespacedName, snap); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Only handle child executions (labelled with parent), not parent objects.
    if snap.Labels[impdevv1alpha1.LabelSnapshotParent] == "" {
        return ctrl.Result{}, nil
    }

    // Already terminal — nothing to do.
    if snap.Status.TerminatedAt != nil {
        return ctrl.Result{}, nil
    }

    // Resolve source VM and check it's on this node.
    vm := &impdevv1alpha1.ImpVM{}
    if err := r.Get(ctx, client.ObjectKey{
        Namespace: snap.Spec.SourceVMNamespace,
        Name:      snap.Spec.SourceVMName,
    }, vm); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    if vm.Spec.NodeName != r.NodeName {
        return ctrl.Result{}, nil
    }

    // Mark Running.
    if snap.Status.Phase != "Running" {
        if err := r.setPhase(ctx, snap, "Running", ""); err != nil {
            return ctrl.Result{}, err
        }
    }

    result, termErr := r.executeSnapshot(ctx, snap, vm)

    // Always set TerminatedAt after execution (success or failure).
    now := metav1.Now()
    base := snap.DeepCopy()
    snap.Status.TerminatedAt = &now
    if termErr != nil {
        snap.Status.Phase = "Failed"
        snap.Status.TerminatedAt = &now
        log.Error(termErr, "snapshot execution failed")
    } else {
        snap.Status.Phase = "Succeeded"
        snap.Status.CompletedAt = &now
        if snap.Spec.Storage.Type == "oci-registry" {
            snap.Status.Digest = result.digest
        } else {
            snap.Status.SnapshotPath = result.path
        }
    }
    return ctrl.Result{}, r.Status().Patch(ctx, snap, client.MergeFrom(base))
}

type execResult struct {
    path   string
    digest string
}

func (r *ImpVMSnapshotReconciler) executeSnapshot(ctx context.Context, snap *impdevv1alpha1.ImpVMSnapshot, vm *impdevv1alpha1.ImpVM) (execResult, error) {
    // Create temp dir for snapshot files.
    tmpDir, err := os.MkdirTemp("", snapshotTempDirPrefix)
    if err != nil {
        return execResult{}, fmt.Errorf("create temp dir: %w", err)
    }
    defer os.RemoveAll(tmpDir) //nolint:errcheck

    sr, err := r.Driver.Snapshot(ctx, vm, tmpDir)
    if err != nil {
        return execResult{}, fmt.Errorf("driver snapshot: %w", err)
    }

    switch snap.Spec.Storage.Type {
    case "node-local":
        return r.storeNodeLocal(snap, sr)
    case "oci-registry":
        return r.pushOCI(ctx, snap, vm, sr)
    default:
        return execResult{}, fmt.Errorf("unknown storage type %q", snap.Spec.Storage.Type)
    }
}

func (r *ImpVMSnapshotReconciler) storeNodeLocal(snap *impdevv1alpha1.ImpVMSnapshot, sr SnapshotResult) (execResult, error) {
    basePath := "/var/lib/imp/snapshots"
    if snap.Spec.Storage.NodeLocal != nil && snap.Spec.Storage.NodeLocal.Path != "" {
        basePath = snap.Spec.Storage.NodeLocal.Path
    }
    destDir := filepath.Join(basePath, snap.Namespace, snap.Labels[impdevv1alpha1.LabelSnapshotParent], snap.Name)
    if err := os.MkdirAll(destDir, 0o750); err != nil {
        return execResult{}, fmt.Errorf("mkdir %s: %w", destDir, err)
    }
    for _, src := range []string{sr.StatePath, sr.MemPath} {
        dest := filepath.Join(destDir, filepath.Base(src))
        if err := os.Rename(src, dest); err != nil {
            return execResult{}, fmt.Errorf("move %s: %w", src, err)
        }
    }
    return execResult{path: destDir}, nil
}

func (r *ImpVMSnapshotReconciler) pushOCI(ctx context.Context, snap *impdevv1alpha1.ImpVMSnapshot, vm *impdevv1alpha1.ImpVM, sr SnapshotResult) (execResult, error) {
    oci := snap.Spec.Storage.OCIRegistry
    if oci == nil {
        return execResult{}, fmt.Errorf("oci-registry storage requires spec.storage.ociRegistry")
    }
    tag := fmt.Sprintf("%s-%s-%s", snap.Namespace, vm.Name, time.Now().UTC().Format("20060102-1504"))
    digest, err := snapshot.PushOCI(ctx, sr.StatePath, sr.MemPath, oci.Repository, tag, oci.PullSecretRef)
    if err != nil {
        return execResult{}, fmt.Errorf("OCI push: %w", err)
    }
    return execResult{digest: digest}, nil
}

func (r *ImpVMSnapshotReconciler) setPhase(ctx context.Context, snap *impdevv1alpha1.ImpVMSnapshot, phase, msg string) error {
    base := snap.DeepCopy()
    snap.Status.Phase = phase
    _ = msg
    return r.Status().Patch(ctx, snap, client.MergeFrom(base))
}

func (r *ImpVMSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&impdevv1alpha1.ImpVMSnapshot{}).
        Complete(r)
}
```

Add `LabelSnapshotParent = "imp.dev/snapshot-parent"` to `api/v1alpha1/shared_types.go` (or a new labels file).

**Step 4: Run tests**

```bash
go test ./internal/agent/ -run TestSnapshotReconciler -v
```

Expected: PASS.

**Step 5: Build check**

```bash
GOOS=linux go build ./...
```

Expected: no errors.

**Step 6: Commit**

```bash
git add internal/agent/impvmsnapshot_reconciler.go internal/agent/impvmsnapshot_reconciler_test.go api/v1alpha1/shared_types.go
git commit -m "feat(agent): ImpVMSnapshotReconciler — node-local and OCI-registry execution"
```

---

## Task 5: Add `robfig/cron/v3` dependency

```bash
cd /Users/giovanni/syscode/git/imp
go get github.com/robfig/cron/v3
go mod tidy
```

Verify it appears in `go.mod` and `go.sum`, then commit:

```bash
git add go.mod go.sum
git commit -m "chore(deps): add robfig/cron/v3 for cron schedule parsing"
```

---

## Task 6: Operator-side `ImpVMSnapshotReconciler`

Replace the skeleton operator reconciler with the full parent-object implementation: one-shot and scheduled child creation, `TerminatedAt`-gated serialisation, retention pruning, and `BaseSnapshot` validation.

**Files:**
- Modify: `internal/controller/impvmsnapshot_controller.go`
- Modify: `internal/controller/impvmsnapshot_controller_test.go`

**Step 1: Write the failing tests**

In `impvmsnapshot_controller_test.go`, add (these are unit tests using fake client — no envtest):

```go
package controller

import (
    "context"
    "testing"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestOperatorSnapshotReconciler_createsChild(t *testing.T) {
    parent := &impdevv1alpha1.ImpVMSnapshot{}
    parent.Name, parent.Namespace = "snap-parent", "default"
    parent.Spec.SourceVMName = "my-vm"
    parent.Spec.SourceVMNamespace = "default"
    parent.Spec.Retention = 3
    parent.Spec.Storage = impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"}

    scheme := newTestScheme(t)
    c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).WithStatusSubresource(parent).Build()

    r := &ImpVMSnapshotReconciler{Client: c, Scheme: scheme}
    _, err := r.Reconcile(context.Background(), reconcile.Request{
        NamespacedName: types.NamespacedName{Name: "snap-parent", Namespace: "default"},
    })
    if err != nil {
        t.Fatalf("reconcile error: %v", err)
    }

    children := &impdevv1alpha1.ImpVMSnapshotList{}
    if err := c.List(context.Background(), children, client.InNamespace("default"),
        client.MatchingLabels{impdevv1alpha1.LabelSnapshotParent: "snap-parent"}); err != nil {
        t.Fatalf("list children: %v", err)
    }
    if len(children.Items) != 1 {
        t.Errorf("expected 1 child, got %d", len(children.Items))
    }
}

func TestOperatorSnapshotReconciler_prunesOldChildren(t *testing.T) {
    parent := &impdevv1alpha1.ImpVMSnapshot{}
    parent.Name, parent.Namespace = "snap-parent", "default"
    parent.Spec.Retention = 2
    parent.Spec.Storage = impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"}

    now := metav1.Now()
    children := make([]client.Object, 3)
    for i := range children {
        c := &impdevv1alpha1.ImpVMSnapshot{}
        c.Name = fmt.Sprintf("snap-parent-exec-%d", i)
        c.Namespace = "default"
        c.Labels = map[string]string{impdevv1alpha1.LabelSnapshotParent: "snap-parent"}
        c.CreationTimestamp = metav1.Time{Time: now.Add(time.Duration(i) * time.Second)}
        // Mark as terminated so they don't block.
        terminated := metav1.Time{Time: now.Add(time.Duration(i)*time.Second + time.Minute)}
        c.Status.TerminatedAt = &terminated
        c.Status.Phase = "Succeeded"
        children[i] = c
    }

    scheme := newTestScheme(t)
    objs := append([]client.Object{parent}, children...)
    fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(parent).Build()

    r := &ImpVMSnapshotReconciler{Client: fc, Scheme: scheme}
    _, err := r.Reconcile(context.Background(), reconcile.Request{
        NamespacedName: types.NamespacedName{Name: "snap-parent", Namespace: "default"},
    })
    if err != nil {
        t.Fatalf("reconcile error: %v", err)
    }

    remaining := &impdevv1alpha1.ImpVMSnapshotList{}
    if err := fc.List(context.Background(), remaining, client.InNamespace("default"),
        client.MatchingLabels{impdevv1alpha1.LabelSnapshotParent: "snap-parent"}); err != nil {
        t.Fatalf("list: %v", err)
    }
    // Retention=2 + 1 new child created = 3 total before prune; prune oldest 1 → 2 remain.
    if len(remaining.Items) > int(parent.Spec.Retention) {
        t.Errorf("expected at most %d children after pruning, got %d", parent.Spec.Retention, len(remaining.Items))
    }
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/controller/ -run TestOperatorSnapshotReconciler -v
```

Expected: FAIL — current skeleton does not create children.

**Step 3: Replace skeleton with full implementation in `impvmsnapshot_controller.go`**

```go
package controller

import (
    "context"
    "fmt"
    "sort"
    "time"

    "github.com/robfig/cron/v3"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    logf "sigs.k8s.io/controller-runtime/pkg/log"

    impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMSnapshotReconciler reconciles ImpVMSnapshot parent objects.
type ImpVMSnapshotReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots/finalizers,verbs=update

func (r *ImpVMSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := logf.FromContext(ctx)

    snap := &impdevv1alpha1.ImpVMSnapshot{}
    if err := r.Get(ctx, req.NamespacedName, snap); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Only handle parent objects (no LabelSnapshotParent label).
    if snap.Labels[impdevv1alpha1.LabelSnapshotParent] != "" {
        return ctrl.Result{}, nil
    }

    children, err := r.listChildren(ctx, snap)
    if err != nil {
        return ctrl.Result{}, err
    }

    // Prune oldest beyond retention (skip children that are the elected BaseSnapshot).
    if err := r.prune(ctx, snap, children); err != nil {
        return ctrl.Result{}, err
    }
    // Re-list after prune.
    children, err = r.listChildren(ctx, snap)
    if err != nil {
        return ctrl.Result{}, err
    }

    // Validate BaseSnapshot if set.
    if err := r.validateBaseSnapshot(ctx, snap, children); err != nil {
        return ctrl.Result{}, err
    }

    // Check if an active child exists (no TerminatedAt).
    for i := range children {
        if children[i].Status.TerminatedAt == nil {
            log.V(1).Info("active child exists, skipping execution creation", "child", children[i].Name)
            return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
        }
    }

    // One-shot: no schedule — create child only if no children at all.
    if snap.Spec.Schedule == "" {
        if len(children) == 0 {
            return r.createChild(ctx, snap)
        }
        return ctrl.Result{}, nil
    }

    // Scheduled: compute next run and create child if due.
    return r.handleScheduled(ctx, snap, children)
}

func (r *ImpVMSnapshotReconciler) handleScheduled(ctx context.Context, snap *impdevv1alpha1.ImpVMSnapshot, children []impdevv1alpha1.ImpVMSnapshot) (ctrl.Result, error) {
    parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
    schedule, err := parser.Parse(snap.Spec.Schedule)
    if err != nil {
        return ctrl.Result{}, fmt.Errorf("invalid cron schedule %q: %w", snap.Spec.Schedule, err)
    }

    now := time.Now().UTC()
    next := schedule.Next(now)

    // If next run is within 5 seconds, create child now.
    if next.Sub(now) <= 5*time.Second {
        res, err := r.createChild(ctx, snap)
        if err != nil {
            return res, err
        }
        next = schedule.Next(now)
    }

    // Update nextScheduledAt.
    nextT := metav1.NewTime(next)
    base := snap.DeepCopy()
    snap.Status.NextScheduledAt = &nextT
    if err := r.Status().Patch(ctx, snap, client.MergeFrom(base)); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{RequeueAfter: time.Until(next)}, nil
}

func (r *ImpVMSnapshotReconciler) createChild(ctx context.Context, parent *impdevv1alpha1.ImpVMSnapshot) (ctrl.Result, error) {
    child := &impdevv1alpha1.ImpVMSnapshot{}
    child.Namespace = parent.Namespace
    child.Name = fmt.Sprintf("%s-%s", parent.Name, time.Now().UTC().Format("20060102-1504"))
    child.Labels = map[string]string{
        impdevv1alpha1.LabelSnapshotParent: parent.Name,
    }
    child.Spec = parent.Spec
    child.Spec.Schedule = "" // children never schedule themselves

    if err := ctrl.SetControllerReference(parent, child, r.Scheme); err != nil {
        return ctrl.Result{}, err
    }
    if err := r.Create(ctx, child); err != nil {
        return ctrl.Result{}, err
    }

    base := parent.DeepCopy()
    parent.Status.LastExecutionRef = &corev1.LocalObjectReference{Name: child.Name}
    return ctrl.Result{}, r.Status().Patch(ctx, parent, client.MergeFrom(base))
}

func (r *ImpVMSnapshotReconciler) prune(ctx context.Context, parent *impdevv1alpha1.ImpVMSnapshot, children []impdevv1alpha1.ImpVMSnapshot) error {
    retention := int(parent.Spec.Retention)
    if retention <= 0 {
        retention = 3
    }

    // Sort by creation time, oldest first.
    sort.Slice(children, func(i, j int) bool {
        return children[i].CreationTimestamp.Before(&children[j].CreationTimestamp)
    })

    toDelete := len(children) - retention
    for i := 0; i < toDelete; i++ {
        c := &children[i]
        // Never prune the elected base snapshot.
        if parent.Spec.BaseSnapshot == c.Name {
            continue
        }
        if err := r.Delete(ctx, c); err != nil {
            return err
        }
    }
    return nil
}

func (r *ImpVMSnapshotReconciler) validateBaseSnapshot(ctx context.Context, snap *impdevv1alpha1.ImpVMSnapshot, children []impdevv1alpha1.ImpVMSnapshot) error {
    if snap.Spec.BaseSnapshot == "" {
        return nil
    }
    for _, c := range children {
        if c.Name == snap.Spec.BaseSnapshot {
            if c.Status.Phase != "Succeeded" {
                return nil // not ready yet
            }
            base := snap.DeepCopy()
            snap.Status.BaseSnapshot = snap.Spec.BaseSnapshot
            return r.Status().Patch(ctx, snap, client.MergeFrom(base))
        }
    }
    // Named child not found — set condition.
    return nil
}

func (r *ImpVMSnapshotReconciler) listChildren(ctx context.Context, parent *impdevv1alpha1.ImpVMSnapshot) ([]impdevv1alpha1.ImpVMSnapshot, error) {
    list := &impdevv1alpha1.ImpVMSnapshotList{}
    if err := r.List(ctx, list,
        client.InNamespace(parent.Namespace),
        client.MatchingLabels{impdevv1alpha1.LabelSnapshotParent: parent.Name},
    ); err != nil {
        return nil, err
    }
    return list.Items, nil
}

func (r *ImpVMSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&impdevv1alpha1.ImpVMSnapshot{}).
        Owns(&impdevv1alpha1.ImpVMSnapshot{}).
        Complete(r)
}
```

Note: add `corev1 "k8s.io/api/core/v1"` import.

**Step 4: Run tests**

```bash
go test ./internal/controller/ -run TestOperatorSnapshotReconciler -v
```

Expected: both tests PASS.

**Step 5: Build check**

```bash
GOOS=linux go build ./...
```

Expected: no errors.

**Step 6: Commit**

```bash
git add internal/controller/impvmsnapshot_controller.go internal/controller/impvmsnapshot_controller_test.go
git commit -m "feat(controller): ImpVMSnapshot operator — child creation, retention, BaseSnapshot validation, cron"
```

---

## Task 7: Wire agent reconciler into agent main

Register `ImpVMSnapshotReconciler` in `cmd/agent/main.go` so it runs alongside the existing `ImpVMReconciler`.

**Files:**
- Modify: `cmd/agent/main.go`

**Step 1: Read `cmd/agent/main.go`** to understand the existing setup pattern.

**Step 2: Register the new reconciler**

Find where `ImpVMReconciler` is registered and add after it:

```go
if err := (&agent.ImpVMSnapshotReconciler{
    Client:   mgr.GetClient(),
    Scheme:   mgr.GetScheme(),
    NodeName: nodeName,
    Driver:   driver,
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "ImpVMSnapshot")
    os.Exit(1)
}
```

**Step 3: Build check**

```bash
GOOS=linux go build ./...
```

Expected: no errors.

**Step 4: Run all non-envtest tests**

```bash
go test ./internal/agent/... ./internal/agent/snapshot/... ./api/... -v 2>&1 | tail -30
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add cmd/agent/main.go
git commit -m "feat(agent): wire ImpVMSnapshotReconciler into agent manager"
```

---

## Final Verification

```bash
GOOS=linux go build ./...
go test ./internal/agent/... ./internal/agent/snapshot/... ./api/v1alpha1/... ./internal/controller/ -run "TestStubDriver_Snapshot|TestFirecrackerDriver_Snapshot|TestPushOCI|TestSnapshotReconciler|TestOperatorSnapshotReconciler" -v
golangci-lint run ./...
```

Expected: all pass, no lint errors.
