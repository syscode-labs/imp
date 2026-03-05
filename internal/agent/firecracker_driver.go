//go:build linux

// internal/agent/firecracker_driver.go
package agent

import (
	"context"
	"errors"
	"fmt"
	gonet "net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/network"
	"github.com/syscode-labs/imp/internal/agent/probe"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
	"github.com/syscode-labs/imp/internal/agent/rootfs"
	agentvsock "github.com/syscode-labs/imp/internal/agent/vsock"
)

// shuttingDownTimeout is how long Stop waits for a graceful ACPI shutdown
// before force-killing the VMM. Declared as var (not const) so tests can override it.
var shuttingDownTimeout = 5 * time.Second

// fcProc holds the runtime state of a running Firecracker microVM process.
type fcProc struct {
	machine      *firecracker.Machine
	pid          int64
	socket       string
	netInfo      *network.NetworkInfo // nil when NetworkRef is absent
	probeCancel  context.CancelFunc   // non-nil when probe goroutine is running
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
	// Net manages host-level networking (bridge, TAP, NAT).
	// May be nil for VMs that do not reference an ImpNetwork.
	Net network.NetManager
	// Alloc manages in-memory IP allocation per ImpNetwork.
	Alloc *network.Allocator
	// GuestAgentPath is the host path to the guest-agent binary for injection.
	// Defaults to rootfs.GuestAgentContainerPath when empty.
	GuestAgentPath string

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
		Alloc:      network.NewAllocator(),
		procs:      make(map[string]*fcProc),
	}, nil
}

// Start implements VMDriver. Fetches the ImpVMClass for compute specs, builds
// an ext4 rootfs from the OCI image, sets up networking if NetworkRef is set,
// then boots a Firecracker microVM. Returns the VMM process PID.
func (d *FirecrackerDriver) Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	if vm.Spec.ClassRef == nil {
		return 0, fmt.Errorf("vm %s/%s has no classRef", vm.Namespace, vm.Name)
	}

	// 1. Fetch compute class.
	var class impdevv1alpha1.ImpVMClass
	if err := d.Client.Get(ctx, ctrlclient.ObjectKey{Name: vm.Spec.ClassRef.Name}, &class); err != nil {
		return 0, fmt.Errorf("get class %q: %w", vm.Spec.ClassRef.Name, err)
	}

	gaEnabled := d.guestAgentEnabled(vm, &class)

	// 2. Build ext4 rootfs from OCI image (cached by digest).
	var buildOpts []rootfs.BuildOption
	if gaEnabled {
		buildOpts = append(buildOpts, rootfs.WithGuestAgent(d.guestAgentPath()))
	}
	rootfsPath, err := d.Cache.Build(ctx, vm.Spec.Image, buildOpts...)
	if err != nil {
		return 0, fmt.Errorf("build rootfs for %s: %w", vm.Spec.Image, err)
	}

	// 3. Set up networking if a NetworkRef is specified.
	var netInfo *network.NetworkInfo
	if vm.Spec.NetworkRef != nil && d.Net != nil {
		ni, err := d.setupNetwork(ctx, vm)
		if err != nil {
			return 0, fmt.Errorf("setup network: %w", err)
		}
		netInfo = ni
	}

	// 4. Ensure socket directory exists.
	if err := os.MkdirAll(d.SocketDir, 0o750); err != nil {
		return 0, fmt.Errorf("socket dir: %w", err)
	}
	sockPath := d.socketPath(vm)

	// 5. Build Firecracker config.
	cfg := d.buildConfig(&class, rootfsPath, sockPath, netInfo, gaEnabled)

	// 6. Build the VMM command.
	cmd := exec.CommandContext(ctx, d.BinPath, "--api-sock", sockPath) //nolint:gosec // G204: BinPath validated in NewFirecrackerDriver

	// 7. Create and start the machine.
	m, err := firecracker.NewMachine(ctx, cfg, firecracker.WithProcessRunner(cmd))
	if err != nil {
		return 0, fmt.Errorf("create machine: %w", err)
	}
	if err := m.Start(ctx); err != nil {
		_ = m.StopVMM()        //nolint:errcheck
		_ = os.Remove(sockPath) //nolint:errcheck // best-effort cleanup
		return 0, fmt.Errorf("start machine: %w", err)
	}

	pid, err := m.PID()
	if err != nil {
		_ = m.StopVMM()        //nolint:errcheck
		_ = os.Remove(sockPath) //nolint:errcheck // best-effort cleanup
		return 0, fmt.Errorf("get pid: %w", err)
	}

	proc := &fcProc{machine: m, pid: int64(pid), socket: sockPath, netInfo: netInfo}

	// Capture probe arguments before goroutine launch to avoid data race on vm.
	var probeCtx context.Context
	var probes *impdevv1alpha1.ProbeSpec
	var vsockPath, vmNamespace, vmName string
	if gaEnabled && vm.Spec.Probes != nil {
		vsockPath = strings.TrimSuffix(sockPath, ".sock") + ".vsock"
		probes = vm.Spec.Probes
		vmNamespace = vm.Namespace
		vmName = vm.Name
		var probeCancel context.CancelFunc
		probeCtx, probeCancel = context.WithCancel(context.Background())
		proc.probeCancel = probeCancel
	}

	// Insert into map before launching the goroutine so Stop() can always find
	// and cancel the probe context, even if called concurrently with Start().
	d.mu.Lock()
	d.procs[vmKey(vm)] = proc
	d.mu.Unlock()

	if probeCtx != nil {
		go d.runProbes(probeCtx, probes, vsockPath, vmNamespace, vmName)
	}

	return int64(pid), nil
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

	if proc.probeCancel != nil {
		proc.probeCancel()
	}

	if proc.machine != nil {
		// Attempt graceful ACPI shutdown with a timeout.
		shutdownCtx, cancel := context.WithTimeout(ctx, shuttingDownTimeout)
		defer cancel()
		_ = proc.machine.Shutdown(shutdownCtx) //nolint:errcheck // best-effort graceful stop

		// Force-kill the VMM process regardless of shutdown result.
		_ = proc.machine.StopVMM() //nolint:errcheck // best-effort force stop
	}

	// Remove the API socket file.
	_ = os.Remove(proc.socket) //nolint:errcheck // best-effort cleanup

	// Tear down networking if this VM had a network attached.
	// NOTE: NAT rules are not torn down in Phase 1 (they are shared across VMs).
	if proc.netInfo != nil {
		if d.Net != nil {
			if err := d.Net.TeardownVM(ctx, proc.netInfo.TAPName); err != nil {
				logf.FromContext(ctx).Error(err, "TeardownVM failed", "tap", proc.netInfo.TAPName)
			}
		}
		if d.Alloc != nil {
			wasLast := d.Alloc.Release(proc.netInfo.NetworkKey, proc.netInfo.IP)
			if wasLast && proc.netInfo.NATEnabled && d.Net != nil {
				if err := d.Net.RemoveNAT(ctx, proc.netInfo.Subnet, proc.netInfo.EgressInterface); err != nil {
					logf.FromContext(ctx).Error(err, "RemoveNAT failed", "subnet", proc.netInfo.Subnet)
				}
			}
		}
	}

	d.mu.Lock()
	delete(d.procs, key)
	d.mu.Unlock()

	return nil
}

// Inspect implements VMDriver. Uses kill(pid,0) to check if the Firecracker
// process is still alive. Returns Running=false for VMs not launched by this driver.
// IP is populated from NetworkInfo when the VM was started with a NetworkRef.
func (d *FirecrackerDriver) Inspect(_ context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error) {
	key := vmKey(vm)

	d.mu.Lock()
	proc, ok := d.procs[key]
	d.mu.Unlock()

	if !ok {
		return VMState{Running: false}, nil
	}

	// kill(pid, 0) — succeeds if process exists; returns ESRCH if not found.
	p, err := os.FindProcess(int(proc.pid))
	if err != nil {
		// Process not found (Unix: never happens; Windows: possible).
		d.mu.Lock()
		delete(d.procs, key)
		d.mu.Unlock()
		return VMState{Running: false}, nil
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			// Process no longer exists — clean up.
			d.mu.Lock()
			delete(d.procs, key)
			d.mu.Unlock()
			return VMState{Running: false}, nil
		}
		return VMState{}, fmt.Errorf("inspect %s: %w", key, err)
	}

	ip := ""
	if proc.netInfo != nil {
		ip = proc.netInfo.IP
	}
	return VMState{Running: true, PID: proc.pid, IP: ip}, nil
}

// socketPath returns the Unix socket path for the given VM.
func (d *FirecrackerDriver) socketPath(vm *impdevv1alpha1.ImpVM) string {
	return filepath.Join(d.SocketDir, vm.Namespace+"-"+vm.Name+".sock")
}

// buildConfig constructs a firecracker.Config for the given VM class and rootfs.
func (d *FirecrackerDriver) buildConfig(
	class *impdevv1alpha1.ImpVMClass,
	rootfsPath, socketPath string,
	netInfo *network.NetworkInfo,
	gaEnabled bool,
) firecracker.Config {
	kernelArgs := d.KernelArgs
	if gaEnabled {
		kernelArgs += " init=/.imp/init"
	}
	cfg := firecracker.Config{
		SocketPath:      socketPath,
		KernelImagePath: d.KernelPath,
		KernelArgs:      kernelArgs,
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
	if netInfo != nil {
		cfg.NetworkInterfaces = firecracker.NetworkInterfaces{{
			StaticConfiguration: &firecracker.StaticNetworkConfiguration{
				MacAddress:  netInfo.MACAddr,
				HostDevName: netInfo.TAPName,
				IPConfiguration: &firecracker.IPConfiguration{
					IPAddr: gonet.IPNet{
						IP:   gonet.ParseIP(netInfo.IP).To4(),
						Mask: gonet.CIDRMask(netInfo.PrefixLen, 32),
					},
					Gateway:     gonet.ParseIP(netInfo.Gateway),
					Nameservers: netInfo.DNS,
				},
			},
		}}
	}
	if gaEnabled {
		vsockPath := strings.TrimSuffix(socketPath, ".sock") + ".vsock"
		cfg.VsockDevices = []firecracker.VsockDevice{{
			ID:   "vsock0",
			Path: vsockPath,
			CID:  3, // guest CID; host always uses CID 2
		}}
	}
	return cfg
}

// setupNetwork fetches the ImpNetwork, allocates an IP, creates the bridge+TAP,
// and optionally installs NAT rules. Returns the NetworkInfo for this VM.
func (d *FirecrackerDriver) setupNetwork(ctx context.Context, vm *impdevv1alpha1.ImpVM) (*network.NetworkInfo, error) {
	var impNet impdevv1alpha1.ImpNetwork
	if err := d.Client.Get(ctx, ctrlclient.ObjectKey{
		Namespace: vm.Namespace,
		Name:      vm.Spec.NetworkRef.Name,
	}, &impNet); err != nil {
		return nil, fmt.Errorf("get network %q: %w", vm.Spec.NetworkRef.Name, err)
	}

	netKey := impNet.Namespace + "/" + impNet.Name
	vKey := vmKey(vm)
	bridgeName := network.BridgeName(netKey)
	tapName := network.TAPName(vKey)
	macAddr := network.MACAddr(vKey)

	_, cidr, err := gonet.ParseCIDR(impNet.Spec.Subnet)
	if err != nil {
		return nil, fmt.Errorf("parse subnet %q: %w", impNet.Spec.Subnet, err)
	}
	prefixLen, _ := cidr.Mask.Size()

	gateway := impNet.Spec.Gateway
	if gateway == "" {
		gw := make(gonet.IP, 4)
		copy(gw, cidr.IP.To4())
		gw[3]++
		gateway = gw.String()
	}

	// Allocate VM IP.
	ip, err := d.Alloc.Allocate(netKey, impNet.Spec.Subnet, gateway)
	if err != nil {
		return nil, fmt.Errorf("allocate IP: %w", err)
	}

	// Ensure bridge exists with gateway IP.
	if err := d.Net.EnsureNetwork(ctx, bridgeName, gateway, prefixLen); err != nil {
		_ = d.Alloc.Release(netKey, ip)
		return nil, fmt.Errorf("ensure bridge: %w", err)
	}

	// Create TAP and attach to bridge.
	if err := d.Net.SetupVM(ctx, tapName, bridgeName, macAddr); err != nil {
		_ = d.Alloc.Release(netKey, ip)
		return nil, fmt.Errorf("setup tap: %w", err)
	}

	// Install NAT if requested (best-effort — don't block VM start on NAT failure).
	if impNet.Spec.NAT.Enabled {
		if natErr := d.Net.EnsureNAT(ctx, impNet.Spec.Subnet, impNet.Spec.NAT.EgressInterface); natErr != nil {
			logf.FromContext(ctx).Error(natErr, "EnsureNAT failed — VM will start without NAT")
		}
	}

	return &network.NetworkInfo{
		TAPName:         tapName,
		BridgeName:      bridgeName,
		MACAddr:         macAddr,
		IP:              ip,
		PrefixLen:       prefixLen,
		Gateway:         gateway,
		DNS:             impNet.Spec.DNS,
		Subnet:          impNet.Spec.Subnet,
		NetworkKey:      netKey,
		NATEnabled:      impNet.Spec.NAT.Enabled,
		EgressInterface: impNet.Spec.NAT.EgressInterface,
	}, nil
}

// guestAgentEnabled returns true when the guest agent should be injected for this VM.
func (d *FirecrackerDriver) guestAgentEnabled(vm *impdevv1alpha1.ImpVM, class *impdevv1alpha1.ImpVMClass) bool {
	return ResolveGuestAgentEnabled(vm, class)
}

// guestAgentPath returns the host path to the guest-agent binary.
func (d *FirecrackerDriver) guestAgentPath() string {
	if d.GuestAgentPath != "" {
		return d.GuestAgentPath
	}
	return rootfs.GuestAgentContainerPath
}

// runProbes dials the guest VSOCK and runs probe polling until ctx is cancelled.
// Called in a goroutine after the VM reaches Running. probes must not be nil.
// vmNamespace and vmName identify the ImpVM to patch conditions onto.
func (d *FirecrackerDriver) runProbes(ctx context.Context, probes *impdevv1alpha1.ProbeSpec, vsockPath, vmNamespace, vmName string) {
	conn, err := agentvsock.Dial(ctx, vsockPath, 10000)
	if err != nil {
		logf.Log.Error(err, "runProbes: VSOCK dial failed", "vsock", vsockPath)
		return
	}
	defer conn.Close() //nolint:errcheck
	client := pb.NewGuestAgentClient(conn)
	runner := probe.NewRunner(client, probes, func(conds []metav1.Condition) {
		nn := types.NamespacedName{Namespace: vmNamespace, Name: vmName}
		vm := &impdevv1alpha1.ImpVM{}
		if err := d.Client.Get(ctx, nn, vm); err != nil {
			logf.Log.Error(err, "probe patcher: Get failed", "vm", nn)
			return
		}
		base := vm.DeepCopy()
		for _, c := range conds {
			apimeta.SetStatusCondition(&vm.Status.Conditions, c)
		}
		if err := d.Client.Status().Patch(ctx, vm, ctrlclient.MergeFrom(base)); err != nil {
			logf.Log.Error(err, "probe patcher: Patch failed", "vm", nn)
		}
	})
	runner.Run(ctx)
}

// compile-time interface check.
var _ VMDriver = (*FirecrackerDriver)(nil)
