# FirecrackerDriver Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `FirecrackerDriver` — a `VMDriver` that boots real Firecracker microVMs from OCI images, completing the Phase 1 agent.

**Architecture:** `FirecrackerDriver` lives in `internal/agent/firecracker_driver.go` alongside `StubDriver`. It fetches compute specs from `ImpVMClass`, builds an ext4 rootfs via `rootfs.Builder`, then launches Firecracker via `firecracker-go-sdk`. Phase 1 uses loopback networking only (no TAP, IP stays empty). Start/Stop/Inspect track live processes via a `pid`-keyed in-memory map; `Inspect` uses `kill(pid, 0)` for liveness.

**Tech Stack:** `github.com/firecracker-microvm/firecracker-go-sdk`, `syscall`, `os/exec`, `sigs.k8s.io/controller-runtime/pkg/client/fake` (tests).

---

## Checklist before starting

```bash
cd /Users/giovanni/syscode/git/imp
pwd   # must end in /imp
which firecracker 2>/dev/null || echo "firecracker not in PATH (OK for dev — tests skip KVM tests)"

# Verify existing packages compile
go build ./...
```

## Context for the implementer

**Key types (already exist — do not modify):**

`VMDriver` interface in `internal/agent/driver.go`:
```go
type VMDriver interface {
    Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (pid int64, err error)
    Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error
    Inspect(ctx context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error)
}
type VMState struct { Running bool; IP string; PID int64 }
```

`vmKey` helper in `internal/agent/stub_driver.go` (package-level, reusable):
```go
func vmKey(vm *impdevv1alpha1.ImpVM) string { return vm.Namespace + "/" + vm.Name }
```

`ImpVMClass` spec fields used by the driver:
```go
class.Spec.VCPU      int32   // virtual CPUs
class.Spec.MemoryMiB int32   // RAM in MiB
```

**Env vars the driver reads (set by DaemonSet):**
- `FC_BIN` — path to firecracker binary (required if not in PATH)
- `FC_SOCK_DIR` — socket directory (default: `/run/imp/sockets`)
- `FC_KERNEL` — path to kernel image (required; typically vmlinux or Image)
- `FC_KERNEL_ARGS` — kernel cmdline (default: `console=ttyS0 reboot=k panic=1 pci=off`)
- `IMP_IMAGE_CACHE` — rootfs cache dir (default: `/var/lib/imp/images`)

**Phase 1 constraints:**
- No TAP networking. `VMState.IP` is always `""`.
- No VSOCK probes.
- Loopback only inside the VM.
- Kernel image must already be present on the node — driver does not fetch it.

---

## Task 1: Add firecracker-go-sdk + FirecrackerDriver skeleton

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/agent/firecracker_driver.go`
- Create: `internal/agent/firecracker_driver_test.go`

### Step 1: Add the dependency

```bash
cd /Users/giovanni/syscode/git/imp
go get github.com/firecracker-microvm/firecracker-go-sdk@latest
go mod tidy
```

Expected: `go.mod` updated with `github.com/firecracker-microvm/firecracker-go-sdk`.

> **Note:** After `go get`, the SDK is available. Verify the exact import paths by checking `go doc github.com/firecracker-microvm/firecracker-go-sdk` or the downloaded source. The plan uses the API as documented at time of writing — adapt if the SDK version differs.

### Step 2: Create the skeleton `firecracker_driver.go`

```go
// internal/agent/firecracker_driver.go
package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/rootfs"
)

// fcProc holds runtime state for a running Firecracker VM.
type fcProc struct {
	machine *firecracker.Machine // used for graceful shutdown
	pid     int64                // used for kill(pid,0) liveness check
	socket  string               // cleaned up on stop
}

// FirecrackerDriver manages real Firecracker VMM processes.
// Safe for concurrent use.
type FirecrackerDriver struct {
	// BinPath is the absolute path to the firecracker binary.
	BinPath string
	// SocketDir is the directory for Firecracker Unix API sockets.
	SocketDir string
	// KernelPath is the path to the guest Linux kernel image (vmlinux / Image).
	KernelPath string
	// KernelArgs is the kernel command line passed to the guest.
	KernelArgs string
	// Cache builds ext4 rootfs images from OCI image references.
	Cache *rootfs.Builder
	// Client fetches ImpVMClass objects from the API server.
	Client ctrlclient.Client

	mu    sync.Mutex
	procs map[string]*fcProc // keyed by vmKey(vm)
}

// NewFirecrackerDriver creates a FirecrackerDriver from environment variables.
// Returns an error if required variables (FC_KERNEL) are missing.
func NewFirecrackerDriver(client ctrlclient.Client) (*FirecrackerDriver, error) {
	binPath := os.Getenv("FC_BIN")
	if binPath == "" {
		p, err := exec.LookPath("firecracker")
		if err != nil {
			return nil, fmt.Errorf("FC_BIN not set and firecracker not in PATH: %w", err)
		}
		binPath = p
	}

	sockDir := os.Getenv("FC_SOCK_DIR")
	if sockDir == "" {
		sockDir = "/run/imp/sockets"
	}

	kernelPath := os.Getenv("FC_KERNEL")
	if kernelPath == "" {
		return nil, fmt.Errorf("FC_KERNEL env var is required")
	}

	kernelArgs := os.Getenv("FC_KERNEL_ARGS")
	if kernelArgs == "" {
		kernelArgs = "console=ttyS0 reboot=k panic=1 pci=off"
	}

	cacheDir := os.Getenv("IMP_IMAGE_CACHE")
	if cacheDir == "" {
		cacheDir = "/var/lib/imp/images"
	}

	return &FirecrackerDriver{
		BinPath:    binPath,
		SocketDir:  sockDir,
		KernelPath: kernelPath,
		KernelArgs: kernelArgs,
		Cache:      &rootfs.Builder{CacheDir: cacheDir},
		Client:     client,
		procs:      make(map[string]*fcProc),
	}, nil
}

// Start implements VMDriver. Not yet implemented — returns error.
func (d *FirecrackerDriver) Start(_ context.Context, _ *impdevv1alpha1.ImpVM) (int64, error) {
	return 0, fmt.Errorf("not implemented")
}

// Stop implements VMDriver. Not yet implemented — returns nil.
func (d *FirecrackerDriver) Stop(_ context.Context, _ *impdevv1alpha1.ImpVM) error {
	return nil
}

// Inspect implements VMDriver. Not yet implemented — returns not running.
func (d *FirecrackerDriver) Inspect(_ context.Context, _ *impdevv1alpha1.ImpVM) (VMState, error) {
	return VMState{Running: false}, nil
}

// socketPath returns the Firecracker API socket path for vm.
func (d *FirecrackerDriver) socketPath(vm *impdevv1alpha1.ImpVM) string {
	return filepath.Join(d.SocketDir, vm.Namespace+"-"+vm.Name+".sock")
}

// buildConfig builds a firecracker.Config for vm using the given compute class.
func (d *FirecrackerDriver) buildConfig(
	class *impdevv1alpha1.ImpVMClass,
	rootfsPath, socketPath string,
) firecracker.Config {
	return firecracker.Config{
		SocketPath:      socketPath,
		KernelImagePath: d.KernelPath,
		KernelArgs:      d.KernelArgs,
		Drives: []models.Drive{
			{
				DriveID:      firecracker.String("rootfs"),
				PathOnHost:   firecracker.String(rootfsPath),
				IsRootDevice: firecracker.Bool(true),
				IsReadOnly:   firecracker.Bool(false),
			},
		},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(int64(class.Spec.VCPU)),
			MemSizeMib: firecracker.Int64(int64(class.Spec.MemoryMiB)),
		},
	}
}

// Ensure FirecrackerDriver implements VMDriver at compile time.
var _ VMDriver = (*FirecrackerDriver)(nil)

// shuttingDownTimeout is the time to wait for ACPI shutdown before force-killing.
var shuttingDownTimeout = 5 * time.Second
```

> **SDK API note:** After `go get`, open the SDK source to verify:
> - `firecracker.String(s)` and `firecracker.Bool(b)` and `firecracker.Int64(n)` helper functions exist
> - `firecracker.Config`, `models.Drive`, `models.MachineConfiguration` field names
> - If any field name differs (e.g. `VcpuCount` vs `VCPUCount`), adapt accordingly
> - The models import path is `github.com/firecracker-microvm/firecracker-go-sdk/client/models`

### Step 3: Create skeleton test file

```go
// internal/agent/firecracker_driver_test.go
package agent

import (
	"testing"
)

// hasFirecrackerBin returns true if the firecracker binary is in PATH or FC_BIN is set.
// Used as a skip guard for tests that require the real VMM.
func hasFirecrackerBin() bool {
	_, err := exec.LookPath("firecracker")
	return err == nil || os.Getenv("FC_BIN") != ""
}

// hasKVM returns true if /dev/kvm is accessible.
// Used as a skip guard for tests that require hardware virtualisation.
func hasKVM() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

func TestFirecrackerDriverPlaceholder(t *testing.T) {
	// Placeholder — replaced in subsequent tasks.
	t.Log("FirecrackerDriver test file compiles correctly")
}
```

(Add `"os"` and `"os/exec"` to imports.)

### Step 4: Verify it compiles

```bash
cd /Users/giovanni/syscode/git/imp && go build ./...
```

Expected: no output. If the SDK helper functions (`firecracker.String`, `firecracker.Bool`, `firecracker.Int64`) do not exist in the downloaded version, adapt `buildConfig` to construct the values inline (e.g. `func ptr[T any](v T) *T { return &v }` or define local helpers).

### Step 5: Commit

```bash
git add go.mod go.sum internal/agent/firecracker_driver.go internal/agent/firecracker_driver_test.go
git commit -m "feat(agent): add FirecrackerDriver skeleton + go-sdk dependency"
```

---

## Task 2: `socketPath` helper test

**Files:**
- Modify: `internal/agent/firecracker_driver_test.go`

### Step 1: Write the test

Add to `firecracker_driver_test.go`:

```go
func TestFirecrackerDriver_SocketPath(t *testing.T) {
	d := &FirecrackerDriver{SocketDir: "/run/imp/sockets"}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "my-vm"

	got := d.socketPath(vm)
	want := "/run/imp/sockets/default-my-vm.sock"
	if got != want {
		t.Errorf("socketPath = %q, want %q", got, want)
	}
}
```

(Add `impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"` to imports.)

### Step 2: Run to verify it passes

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run TestFirecrackerDriver_SocketPath -v -count=1
```

Expected: PASS.

### Step 3: Commit

```bash
git add internal/agent/firecracker_driver_test.go
git commit -m "test(agent): socketPath helper test"
```

---

## Task 3: `buildConfig` helper test

**Files:**
- Modify: `internal/agent/firecracker_driver_test.go`

### Step 1: Write the test

```go
func TestFirecrackerDriver_BuildConfig(t *testing.T) {
	d := &FirecrackerDriver{
		KernelPath: "/boot/vmlinux",
		KernelArgs: "console=ttyS0 reboot=k panic=1 pci=off",
	}

	class := &impdevv1alpha1.ImpVMClass{}
	class.Spec.VCPU = 2
	class.Spec.MemoryMiB = 512

	cfg := d.buildConfig(class, "/cache/abc.ext4", "/run/imp/sockets/default-vm.sock")

	if cfg.SocketPath != "/run/imp/sockets/default-vm.sock" {
		t.Errorf("SocketPath = %q", cfg.SocketPath)
	}
	if cfg.KernelImagePath != "/boot/vmlinux" {
		t.Errorf("KernelImagePath = %q", cfg.KernelImagePath)
	}
	if cfg.KernelArgs != "console=ttyS0 reboot=k panic=1 pci=off" {
		t.Errorf("KernelArgs = %q", cfg.KernelArgs)
	}
	if len(cfg.Drives) != 1 {
		t.Fatalf("expected 1 drive, got %d", len(cfg.Drives))
	}
	if *cfg.Drives[0].PathOnHost != "/cache/abc.ext4" {
		t.Errorf("Drive path = %q", *cfg.Drives[0].PathOnHost)
	}
	if *cfg.Drives[0].IsRootDevice != true {
		t.Error("drive should be root device")
	}
	if *cfg.MachineCfg.VcpuCount != 2 {
		t.Errorf("VcpuCount = %d", *cfg.MachineCfg.VcpuCount)
	}
	if *cfg.MachineCfg.MemSizeMib != 512 {
		t.Errorf("MemSizeMib = %d", *cfg.MachineCfg.MemSizeMib)
	}
}
```

### Step 2: Run to verify it passes

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run TestFirecrackerDriver_BuildConfig -v -count=1
```

Expected: PASS. If field names differ in the SDK version you downloaded, adapt the test to match the actual field names.

### Step 3: Commit

```bash
git add internal/agent/firecracker_driver_test.go
git commit -m "test(agent): buildConfig helper test"
```

---

## Task 4: `Inspect` — `kill(pid,0)` liveness check

**Files:**
- Modify: `internal/agent/firecracker_driver.go` — replace stub Inspect
- Modify: `internal/agent/firecracker_driver_test.go` — add 3 tests

### Step 1: Write the failing tests

```go
func TestFirecrackerDriver_Inspect_NotTracked(t *testing.T) {
	d := &FirecrackerDriver{procs: make(map[string]*fcProc)}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "ghost"

	state, err := d.Inspect(context.Background(), vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Running {
		t.Error("expected Running=false for untracked VM")
	}
}

func TestFirecrackerDriver_Inspect_LiveProcess(t *testing.T) {
	d := &FirecrackerDriver{procs: make(map[string]*fcProc)}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "live"

	// Inject the current process PID — guaranteed to be alive.
	d.procs[vmKey(vm)] = &fcProc{pid: int64(os.Getpid())}

	state, err := d.Inspect(context.Background(), vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !state.Running {
		t.Error("expected Running=true for live process")
	}
	if state.PID != int64(os.Getpid()) {
		t.Errorf("PID = %d, want %d", state.PID, os.Getpid())
	}
}

func TestFirecrackerDriver_Inspect_DeadProcess(t *testing.T) {
	d := &FirecrackerDriver{procs: make(map[string]*fcProc)}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "dead"

	// Use a PID that almost certainly does not exist.
	d.procs[vmKey(vm)] = &fcProc{pid: 2147483647}

	state, err := d.Inspect(context.Background(), vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Running {
		t.Error("expected Running=false for dead process")
	}
	// After detecting the dead process, it should be cleaned up from the map.
	d.mu.Lock()
	_, stillTracked := d.procs[vmKey(vm)]
	d.mu.Unlock()
	if stillTracked {
		t.Error("expected dead process to be removed from procs map")
	}
}
```

(Add `"context"` to imports.)

### Step 2: Run to verify they fail

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run "TestFirecrackerDriver_Inspect" -v -count=1
```

Expected: the first test may pass (stub returns `Running=false`) but the live/dead tests will fail.

### Step 3: Implement `Inspect`

Replace the stub `Inspect` in `firecracker_driver.go`:

```go
// Inspect implements VMDriver. Uses kill(pid,0) to check if the Firecracker
// process is still alive. Returns Running=false for VMs not launched by this driver.
// Phase 1: IP is always empty (loopback only, no TAP).
func (d *FirecrackerDriver) Inspect(_ context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error) {
	d.mu.Lock()
	proc, ok := d.procs[vmKey(vm)]
	d.mu.Unlock()

	if !ok {
		return VMState{Running: false}, nil
	}

	// kill(pid, 0) — succeeds if process exists; returns ESRCH if not found.
	p, err := os.FindProcess(int(proc.pid))
	if err != nil {
		// Process not found (Unix: never happens; Windows: possible).
		d.mu.Lock()
		delete(d.procs, vmKey(vm))
		d.mu.Unlock()
		return VMState{Running: false}, nil
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			// Process no longer exists — clean up.
			d.mu.Lock()
			delete(d.procs, vmKey(vm))
			d.mu.Unlock()
			return VMState{Running: false}, nil
		}
		return VMState{}, fmt.Errorf("inspect %s: %w", vmKey(vm), err)
	}

	// Phase 1: IP is empty (no TAP networking).
	return VMState{Running: true, PID: proc.pid}, nil
}
```

Add `"errors"` to imports.

### Step 4: Run to verify all 3 tests pass

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run "TestFirecrackerDriver_Inspect" -v -count=1
```

Expected: all 3 PASS.

### Step 5: Run linter

```bash
cd /Users/giovanni/syscode/git/imp && golangci-lint run ./internal/agent/
```

Expected: 0 issues.

### Step 6: Commit

```bash
git add internal/agent/firecracker_driver.go internal/agent/firecracker_driver_test.go
git commit -m "feat(agent): FirecrackerDriver.Inspect — kill(pid,0) liveness check"
```

---

## Task 5: `Stop` — graceful ACPI → force kill → socket cleanup

**Files:**
- Modify: `internal/agent/firecracker_driver.go` — replace stub Stop
- Modify: `internal/agent/firecracker_driver_test.go` — add test

### Step 1: Write the failing test

```go
func TestFirecrackerDriver_Stop_NotTracked(t *testing.T) {
	d := &FirecrackerDriver{procs: make(map[string]*fcProc)}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "ghost"

	// Stop on an untracked VM should be a no-op.
	if err := d.Stop(context.Background(), vm); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
```

### Step 2: Run to verify it passes (stub returns nil already)

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run TestFirecrackerDriver_Stop_NotTracked -v -count=1
```

Expected: PASS (the stub already returns nil; this test verifies the final implementation also handles this case).

### Step 3: Implement `Stop`

Replace the stub `Stop` in `firecracker_driver.go`:

```go
// Stop implements VMDriver. Sends a graceful ACPI shutdown signal, waits up to
// 5 seconds, then force-terminates the Firecracker process. Cleans up the Unix socket.
// Safe to call on a VM that was never started or already stopped.
func (d *FirecrackerDriver) Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	key := vmKey(vm)

	d.mu.Lock()
	proc, ok := d.procs[key]
	d.mu.Unlock()

	if !ok {
		return nil // already stopped or never started
	}

	// Attempt graceful ACPI shutdown with a timeout.
	shutdownCtx, cancel := context.WithTimeout(ctx, shuttingDownTimeout)
	defer cancel()
	_ = proc.machine.Shutdown(shutdownCtx) //nolint:errcheck // best-effort graceful stop

	// Force-kill the VMM process regardless of shutdown result.
	_ = proc.machine.StopVMM() //nolint:errcheck // best-effort force stop

	// Remove the API socket file.
	_ = os.Remove(proc.socket) //nolint:errcheck // best-effort cleanup

	d.mu.Lock()
	delete(d.procs, key)
	d.mu.Unlock()

	return nil
}
```

### Step 4: Run all driver tests

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run "TestFirecrackerDriver" -v -count=1
```

Expected: all pass.

### Step 5: Run linter

```bash
golangci-lint run ./internal/agent/
```

Expected: 0 issues.

### Step 6: Commit

```bash
git add internal/agent/firecracker_driver.go internal/agent/firecracker_driver_test.go
git commit -m "feat(agent): FirecrackerDriver.Stop — graceful ACPI + force kill + socket cleanup"
```

---

## Task 6: `Start` — class fetch + rootfs build + Firecracker boot

**Files:**
- Modify: `internal/agent/firecracker_driver.go` — replace stub Start
- Modify: `internal/agent/firecracker_driver_test.go` — add tests

### Step 1: Write the failing tests

These tests use `sigs.k8s.io/controller-runtime/pkg/client/fake` to avoid needing a real API server.

```go
func TestFirecrackerDriver_Start_NoClassRef(t *testing.T) {
	d := &FirecrackerDriver{procs: make(map[string]*fcProc)}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "test"
	// vm.Spec.ClassRef is nil

	_, err := d.Start(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error for missing ClassRef")
	}
}

func TestFirecrackerDriver_Start_ClassNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	d := &FirecrackerDriver{
		Client: fakeClient,
		procs:  make(map[string]*fcProc),
	}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "test"
	vm.Spec.ClassRef = &impdevv1alpha1.ClusterObjectRef{Name: "nonexistent"}

	_, err := d.Start(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error when class not found")
	}
}

func TestFirecrackerDriver_Start_RootfsBuildFails(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	class := &impdevv1alpha1.ImpVMClass{}
	class.Name = "small"
	class.Spec.VCPU = 1
	class.Spec.MemoryMiB = 256

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build()

	d := &FirecrackerDriver{
		Client: fakeClient,
		Cache:  &rootfs.Builder{CacheDir: t.TempDir(), Insecure: true},
		procs:  make(map[string]*fcProc),
	}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "test"
	vm.Spec.ClassRef = &impdevv1alpha1.ClusterObjectRef{Name: "small"}
	vm.Spec.Image = "localhost:9999/nonexistent:latest" // unreachable registry

	_, err := d.Start(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error when rootfs build fails")
	}
}

func TestFirecrackerDriver_Start_Integration(t *testing.T) {
	if !hasFirecrackerBin() {
		t.Skip("firecracker binary not available")
	}
	if !hasKVM() {
		t.Skip("/dev/kvm not accessible")
	}
	t.Log("Full integration test: requires real Firecracker + KVM + kernel image")
	// Full pipeline test: FC_KERNEL must point to a real kernel image.
	// This test is validated on real hardware (OCI Ampere ARM64 or Lenovo M720q).
	t.Skip("integration test — run manually on KVM-capable node")
}
```

Add these imports:
```go
import (
    "k8s.io/apimachinery/pkg/runtime"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"
    "github.com/syscode-labs/imp/internal/agent/rootfs"
)
```

### Step 2: Run to verify failures

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run "TestFirecrackerDriver_Start" -v -count=1
```

Expected: `Start_NoClassRef` passes (stub returns error), others fail.

### Step 3: Implement `Start`

Replace the stub `Start` in `firecracker_driver.go`:

```go
// Start implements VMDriver. Fetches the ImpVMClass for compute specs, builds
// an ext4 rootfs from the OCI image, then boots a Firecracker microVM.
// Phase 1: loopback networking only. Returns the VMM process PID.
func (d *FirecrackerDriver) Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	if vm.Spec.ClassRef == nil {
		return 0, fmt.Errorf("vm %s/%s has no classRef", vm.Namespace, vm.Name)
	}

	// 1. Fetch compute class.
	var class impdevv1alpha1.ImpVMClass
	if err := d.Client.Get(ctx, ctrlclient.ObjectKey{Name: vm.Spec.ClassRef.Name}, &class); err != nil {
		return 0, fmt.Errorf("get class %q: %w", vm.Spec.ClassRef.Name, err)
	}

	// 2. Build ext4 rootfs from OCI image (cached by digest).
	rootfsPath, err := d.Cache.Build(ctx, vm.Spec.Image)
	if err != nil {
		return 0, fmt.Errorf("build rootfs for %s: %w", vm.Spec.Image, err)
	}

	// 3. Ensure socket directory exists.
	if err := os.MkdirAll(d.SocketDir, 0o750); err != nil {
		return 0, fmt.Errorf("socket dir: %w", err)
	}
	sockPath := d.socketPath(vm)

	// 4. Build Firecracker config.
	cfg := d.buildConfig(&class, rootfsPath, sockPath)

	// 5. Build the VMM command (passes --api-sock to the firecracker process).
	cmd := exec.CommandContext(ctx, d.BinPath, "--api-sock", sockPath) //nolint:gosec // G204: BinPath is validated via LookPath in NewFirecrackerDriver

	// 6. Create and start the machine.
	m, err := firecracker.NewMachine(ctx, cfg, firecracker.WithProcessRunner(cmd))
	if err != nil {
		return 0, fmt.Errorf("create machine: %w", err)
	}
	if err := m.Start(ctx); err != nil {
		_ = m.StopVMM() //nolint:errcheck
		return 0, fmt.Errorf("start machine: %w", err)
	}

	pid := int64(m.PID())

	d.mu.Lock()
	d.procs[vmKey(vm)] = &fcProc{machine: m, pid: pid, socket: sockPath}
	d.mu.Unlock()

	return pid, nil
}
```

### Step 4: Run to verify all Start tests pass

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run "TestFirecrackerDriver_Start" -v -count=1
```

Expected: `Start_NoClassRef` PASS, `Start_ClassNotFound` PASS, `Start_RootfsBuildFails` PASS, `Start_Integration` SKIP.

### Step 5: Run all driver tests

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/agent/ -run "TestFirecrackerDriver" -v -count=1
```

Expected: all pass or skip.

### Step 6: Lint

```bash
golangci-lint run ./internal/agent/
```

Expected: 0 issues.

### Step 7: Commit

```bash
git add internal/agent/firecracker_driver.go internal/agent/firecracker_driver_test.go
git commit -m "feat(agent): FirecrackerDriver.Start — class fetch + rootfs build + Firecracker boot"
```

---

## Task 7: Wire `FirecrackerDriver` into `cmd/agent/main.go`

**Files:**
- Modify: `cmd/agent/main.go`

### Step 1: Replace the placeholder in main.go

Find the `else` branch in `cmd/agent/main.go` that currently falls back to StubDriver:

```go
} else {
    // Phase 2 will replace this with FirecrackerDriver.
    log.Info("FirecrackerDriver not yet implemented — using StubDriver (set IMP_STUB_DRIVER=true to suppress this warning)")
    driver = agent.NewStubDriver()
}
```

Replace it with:

```go
} else {
    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        Scheme: scheme,
        // ... (this block creates the manager before the driver is built)
    })
    // NOTE: We need the manager's client to create FirecrackerDriver.
    // Restructure: build FirecrackerDriver after the manager is created.
}
```

**Wait — restructuring is needed** because `NewFirecrackerDriver` needs a `ctrlclient.Client`, which comes from the controller-runtime manager. The manager is created later. Restructure `main.go` as follows:

1. Build the scheme.
2. Create the manager (already done — uses `ctrl.NewManager`).
3. Create the driver using `mgr.GetClient()`.
4. Register the reconciler.

Here is the **complete new `cmd/agent/main.go`** (replace the file entirely):

```go
package main

import (
	"flag"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
)

func main() {
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("agent")

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error(nil, "NODE_NAME env var not set — run as DaemonSet with fieldRef downward API")
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Error(err, "Unable to add client-go scheme")
		os.Exit(1)
	}
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		log.Error(err, "Unable to add imp scheme")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false,
	})
	if err != nil {
		log.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	// IMP_STUB_DRIVER=true: StubDriver (CI, test clusters, no KVM needed).
	// Otherwise: FirecrackerDriver (reads FC_BIN, FC_SOCK_DIR, FC_KERNEL env vars).
	var driver agent.VMDriver
	if os.Getenv("IMP_STUB_DRIVER") == "true" {
		log.Info("Using StubDriver (IMP_STUB_DRIVER=true)")
		driver = agent.NewStubDriver()
	} else {
		fc, err := agent.NewFirecrackerDriver(mgr.GetClient())
		if err != nil {
			log.Error(err, "Unable to create FirecrackerDriver — set FC_KERNEL and ensure FC_BIN is in PATH")
			os.Exit(1)
		}
		log.Info("Using FirecrackerDriver", "bin", fc.BinPath, "sockDir", fc.SocketDir, "kernel", fc.KernelPath)
		driver = fc
	}

	if err := (&agent.ImpVMReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		NodeName: nodeName,
		Driver:   driver,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "Unable to set up ImpVMReconciler")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up ready check")
		os.Exit(1)
	}

	log.Info("Agent starting", "node", nodeName)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "Problem running agent manager")
		os.Exit(1)
	}
}
```

### Step 2: Verify it compiles

```bash
cd /Users/giovanni/syscode/git/imp && go build ./cmd/agent/
```

Expected: no output.

### Step 3: Run full test suite

```bash
KUBEBUILDER_ASSETS="/Users/giovanni/syscode/git/imp/bin/k8s/k8s/1.35.0-darwin-amd64" \
  go test ./... -count=1
```

Expected: all packages pass.

### Step 4: Run linter

```bash
golangci-lint run ./...
```

Expected: 0 issues.

### Step 5: Commit

```bash
git add cmd/agent/main.go
git commit -m "feat(agent): wire FirecrackerDriver into main.go — reads FC_KERNEL/FC_BIN/FC_SOCK_DIR"
```

---

## Task 8: Full test suite verification

### Step 1: Run all tests

```bash
KUBEBUILDER_ASSETS="/Users/giovanni/syscode/git/imp/bin/k8s/k8s/1.35.0-darwin-amd64" \
  go test ./... -count=1 -v 2>&1 | tail -30
```

Expected: all packages pass. KVM tests skip.

### Step 2: go mod tidy

```bash
cd /Users/giovanni/syscode/git/imp && go mod tidy
```

If go.mod/go.sum changed, stage and commit:
```bash
git add go.mod go.sum
git commit -m "chore: go mod tidy"
```

### Step 3: Final lint check

```bash
golangci-lint run ./...
```

Expected: 0 issues.

### Step 4: Report git log

```bash
git log --oneline -10
```

---

## Notes for the implementer

**firecracker-go-sdk API verification:** After `go get`, the exact method names may differ from what's in this plan. Run:
```bash
go doc github.com/firecracker-microvm/firecracker-go-sdk | head -80
go doc github.com/firecracker-microvm/firecracker-go-sdk.Machine
```
Key methods to confirm: `NewMachine`, `Machine.Start`, `Machine.Shutdown`, `Machine.StopVMM`, `Machine.PID`. If `Shutdown` takes no context, remove the context argument.

**`firecracker.String` / `Bool` / `Int64` helpers:** These are pointer helpers (`func String(s string) *string`). If they don't exist in the version you downloaded, define local equivalents:
```go
func fcStr(s string) *string { return &s }
func fcBool(b bool) *bool    { return &b }
func fcInt64(i int64) *int64 { return &i }
```

**`shuttingDownTimeout`:** Declared as `var` (not `const`) so tests can override it with `defer`.

**Phase 1 constraint:** `VMState.IP` is always `""` in `Inspect`. TAP networking and IP assignment are Phase 2. The reconciler handles empty IP gracefully — it just won't set `status.ip`.

**Test structure:** `TestFirecrackerDriver_Start_*` tests use `sigs.k8s.io/controller-runtime/pkg/client/fake` — a fully in-memory fake Kubernetes client. No envtest needed. Import: `"sigs.k8s.io/controller-runtime/pkg/client/fake"`.

**`hasFirecrackerBin` and `hasKVM`** are defined in `firecracker_driver_test.go` and used as skip guards. The integration test (`Start_Integration`) always skips — it's a placeholder for manual validation on real hardware.
