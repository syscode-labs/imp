//go:build linux

// internal/agent/firecracker_driver.go
package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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

// Stop halts the VM. Not yet implemented (no-op).
func (d *FirecrackerDriver) Stop(_ context.Context, _ *impdevv1alpha1.ImpVM) error {
	return nil
}

// Inspect returns the current runtime state of the VM. Not yet implemented.
func (d *FirecrackerDriver) Inspect(_ context.Context, _ *impdevv1alpha1.ImpVM) (VMState, error) {
	return VMState{Running: false}, nil
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
