//go:build linux

package agent

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/rootfs"
)

// hasFirecrackerBin returns true if the firecracker binary is available.
func hasFirecrackerBin() bool {
	_, err := exec.LookPath("firecracker")
	return err == nil || os.Getenv("FC_BIN") != ""
}

// hasKVM returns true if /dev/kvm is accessible.
func hasKVM() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

func TestFirecrackerDriverPlaceholder(t *testing.T) {
	t.Log("FirecrackerDriver test file compiles correctly")
}

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
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, "/run/imp/sockets/default-vm.sock")
	}
	if cfg.KernelImagePath != "/boot/vmlinux" {
		t.Errorf("KernelImagePath = %q, want %q", cfg.KernelImagePath, "/boot/vmlinux")
	}
	if cfg.KernelArgs != "console=ttyS0 reboot=k panic=1 pci=off" {
		t.Errorf("KernelArgs = %q, want %q", cfg.KernelArgs, "console=ttyS0 reboot=k panic=1 pci=off")
	}

	if len(cfg.Drives) != 1 {
		t.Fatalf("len(cfg.Drives) = %d, want 1", len(cfg.Drives))
	}
	if *cfg.Drives[0].PathOnHost != "/cache/abc.ext4" {
		t.Errorf("Drives[0].PathOnHost = %q, want %q", *cfg.Drives[0].PathOnHost, "/cache/abc.ext4")
	}
	if *cfg.Drives[0].IsRootDevice != true {
		t.Errorf("Drives[0].IsRootDevice = %v, want true", *cfg.Drives[0].IsRootDevice)
	}

	if *cfg.MachineCfg.VcpuCount != 2 {
		t.Errorf("MachineCfg.VcpuCount = %d, want 2", *cfg.MachineCfg.VcpuCount)
	}
	if *cfg.MachineCfg.MemSizeMib != 512 {
		t.Errorf("MachineCfg.MemSizeMib = %d, want 512", *cfg.MachineCfg.MemSizeMib)
	}
}

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
	t.Skip("integration test — run manually on KVM-capable node")
}
