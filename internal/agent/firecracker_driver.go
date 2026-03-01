//go:build linux

// internal/agent/firecracker_driver.go
package agent

import (
	"context"
	"errors"
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

// shuttingDownTimeout is how long Stop waits for a graceful ACPI shutdown
// before force-killing the VMM. Declared as var (not const) so tests can override it.
var shuttingDownTimeout = 5 * time.Second

// fcProc holds the runtime state of a running Firecracker microVM process.
type fcProc struct {
	machine *firecracker.Machine
	pid     int64
	socket  string
}

// FirecrackerDriver is a VMDriver that launches real Firecracker microVMs.
// It is safe for concurrent use.
type FirecrackerDriver struct {
	// BinPath is the path to the firecracker binary.
	BinPath string
	// SocketDir is the directory where per-VM Unix sockets are created.
	SocketDir string
	// KernelPath is the path to the uncompressed ELF kernel image.
	KernelPath string
	// KernelArgs is the kernel command-line string.
	KernelArgs string
	// Cache builds and caches ext4 rootfs images from OCI images.
	Cache *rootfs.Builder
	// Client is the controller-runtime Kubernetes client.
	Client ctrlclient.Client

	// mu guards procs. Must be held for any read or write of the procs map.
	mu    sync.Mutex
	procs map[string]*fcProc // keyed by vmKey(vm)
}

// NewFirecrackerDriver constructs a FirecrackerDriver from environment variables:
//   - FC_BIN         — path to firecracker binary (falls back to exec.LookPath)
//   - FC_SOCK_DIR    — socket directory (default: /run/imp/sockets)
//   - FC_KERNEL      — required: path to the kernel image
//   - FC_KERNEL_ARGS — kernel args (default: console=ttyS0 reboot=k panic=1 pci=off)
//   - IMP_IMAGE_CACHE — rootfs cache dir (default: /var/lib/imp/images)
func NewFirecrackerDriver(client ctrlclient.Client) (*FirecrackerDriver, error) {
	binPath := os.Getenv("FC_BIN")
	if binPath == "" {
		found, err := exec.LookPath("firecracker")
		if err != nil {
			return nil, fmt.Errorf("firecracker binary not found (set FC_BIN or install firecracker): %w", err)
		}
		binPath = found
	}

	socketDir := os.Getenv("FC_SOCK_DIR")
	if socketDir == "" {
		socketDir = "/run/imp/sockets"
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
		SocketDir:  socketDir,
		KernelPath: kernelPath,
		KernelArgs: kernelArgs,
		Cache:      &rootfs.Builder{CacheDir: cacheDir},
		Client:     client,
		procs:      make(map[string]*fcProc),
	}, nil
}

// Start boots the VM and returns its runtime PID. Not yet implemented.
func (d *FirecrackerDriver) Start(_ context.Context, _ *impdevv1alpha1.ImpVM) (int64, error) {
	return 0, fmt.Errorf("not implemented")
}

// Stop implements VMDriver. Sends a graceful ACPI shutdown signal, waits up to
// shuttingDownTimeout, then force-terminates the Firecracker process and cleans up
// the Unix socket. Safe to call on a VM that was never started or already stopped.
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

// socketPath returns the Unix socket path for the given VM.
func (d *FirecrackerDriver) socketPath(vm *impdevv1alpha1.ImpVM) string {
	return filepath.Join(d.SocketDir, vm.Namespace+"-"+vm.Name+".sock")
}

// buildConfig constructs a firecracker.Config for the given VM class and rootfs.
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

// compile-time interface check.
var _ VMDriver = (*FirecrackerDriver)(nil)
