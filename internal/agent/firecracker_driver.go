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
	fcclientops "github.com/firecracker-microvm/firecracker-go-sdk/client/operations"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/network"
	"github.com/syscode-labs/imp/internal/agent/probe"
	"github.com/syscode-labs/imp/internal/agent/rootfs"
	agentvsock "github.com/syscode-labs/imp/internal/agent/vsock"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
	"github.com/syscode-labs/imp/internal/tracing"
)

// shuttingDownTimeout is how long Stop waits for a graceful ACPI shutdown
// before force-killing the VMM. Declared as var (not const) so tests can override it.
var shuttingDownTimeout = 5 * time.Second

// metricsInterval controls how often guest metrics are polled via VSOCK.
// Declared as var (not const) so tests can override it.
var metricsInterval = 30 * time.Second

// fcProc holds the runtime state of a running Firecracker microVM process.
type fcProc struct {
	machine     *firecracker.Machine
	pid         int64
	socket      string
	vsockPath   string               // path to the VSOCK Unix socket proxy; empty when guest agent is disabled
	netInfo     *network.NetworkInfo // nil when NetworkRef is absent
	probeCancel context.CancelFunc   // non-nil when probe goroutine is running
}

type rootfsBuilder interface {
	Build(ctx context.Context, imageRef string, opts ...rootfs.BuildOption) (string, error)
	BuildComposite(ctx context.Context, baseImage string, extraLayers []string, opts ...rootfs.BuildOption) (string, error)
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
	Cache rootfsBuilder
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
	// Metrics collects Prometheus gauges for guest CPU/mem/disk.
	// When nil, guest metrics polling is skipped.
	Metrics *VMMetricsCollector
	// NodeName is the Kubernetes node name, used as a Prometheus label in guest metrics.
	NodeName string

	// mu guards procs. Must be held for any read or write of the procs map.
	mu    sync.Mutex
	procs map[string]*fcProc // keyed by vmKey(vm)
}

// compile-time interface check.
var _ VMDriver = (*FirecrackerDriver)(nil)

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
	var rootfsPath string
	{
		rCtx, rSpan := otel.Tracer("imp.agent").Start(ctx, "agent.impvm.rootfs_build",
			trace.WithAttributes(
				attribute.String("vm.name", vm.Name),
				attribute.String("vm.image", vm.Spec.Image),
			),
		)
		var buildErr error
		rootfsPath, buildErr = d.buildRootfs(rCtx, vm, buildOpts...)
		tracing.RecordError(rSpan, buildErr)
		rSpan.End()
		if buildErr != nil {
			return 0, fmt.Errorf("build rootfs for %s: %w", vm.Spec.Image, buildErr)
		}
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

	// 5a. Apply snapshot-based boot if requested.
	if vm.Spec.SnapshotRef != "" {
		if err := d.applySnapshotBoot(ctx, vm, &cfg); err != nil {
			return 0, fmt.Errorf("apply snapshot boot: %w", err)
		}
	}

	// 6. Build the VMM command.
	cmd := exec.CommandContext(ctx, d.BinPath, "--api-sock", sockPath) //nolint:gosec // G204: BinPath validated in NewFirecrackerDriver

	// Redirect the Firecracker process stdout to a serial log file so that
	// the guest ttyS0 console (console=ttyS0 kernel arg) is persisted on disk.
	serialLogPath := filepath.Join(d.SocketDir, vm.Namespace+"-"+vm.Name+".serial.log")
	serialLogFile, err := os.OpenFile(serialLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640) //nolint:gosec // G304: path is derived from Kubernetes metadata names
	if err != nil {
		return 0, fmt.Errorf("open serial log %s: %w", serialLogPath, err)
	}
	cmd.Stdout = serialLogFile
	defer serialLogFile.Close() //nolint:errcheck // child inherits fd; parent closing its copy is safe

	// 7. Create and start the machine.
	m, err := firecracker.NewMachine(ctx, cfg, firecracker.WithProcessRunner(cmd))
	if err != nil {
		return 0, fmt.Errorf("create machine: %w", err)
	}
	{
		lCtx, lSpan := otel.Tracer("imp.agent").Start(ctx, "agent.impvm.firecracker_launch",
			trace.WithAttributes(
				attribute.String("vm.name", vm.Name),
				attribute.String("vm.namespace", vm.Namespace),
			),
		)
		startErr := m.Start(lCtx)
		tracing.RecordError(lSpan, startErr)
		lSpan.End()
		if startErr != nil {
			_ = m.StopVMM()         //nolint:errcheck
			_ = os.Remove(sockPath) //nolint:errcheck // best-effort cleanup
			return 0, fmt.Errorf("start machine: %w", startErr)
		}
	}

	pid, err := m.PID()
	if err != nil {
		_ = m.StopVMM()         //nolint:errcheck
		_ = os.Remove(sockPath) //nolint:errcheck // best-effort cleanup
		return 0, fmt.Errorf("get pid: %w", err)
	}

	proc := &fcProc{machine: m, pid: int64(pid), socket: sockPath, netInfo: netInfo}

	// Capture goroutine args before launch to avoid data race on vm.
	var probeCtx context.Context
	var probes *impdevv1alpha1.ProbeSpec
	var vsockPath, vmNamespace, vmName, className string
	if gaEnabled {
		vsockPath = strings.TrimSuffix(sockPath, ".sock") + ".vsock"
		proc.vsockPath = vsockPath
		probes = vm.Spec.Probes // may be nil — runProbes handles nil probes
		vmNamespace = vm.Namespace
		vmName = vm.Name
		className = vm.Spec.ClassRef.Name // safe: checked non-nil at top of Start
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
		go d.runProbes(probeCtx, probes, vsockPath, vmNamespace, vmName, className)
	}

	return int64(pid), nil
}

func (d *FirecrackerDriver) buildRootfs(
	ctx context.Context, vm *impdevv1alpha1.ImpVM, opts ...rootfs.BuildOption,
) (string, error) {
	extraLayers := make([]string, 0, 2)
	if vm.Spec.RunnerLayer != "" {
		extraLayers = append(extraLayers, vm.Spec.RunnerLayer)
	}
	if vm.Spec.CiliumLayer != "" {
		extraLayers = append(extraLayers, vm.Spec.CiliumLayer)
	}
	if len(extraLayers) == 0 {
		return d.Cache.Build(ctx, vm.Spec.Image, opts...)
	}
	return d.Cache.BuildComposite(ctx, vm.Spec.Image, extraLayers, opts...)
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
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
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

	allocSubnet, err := resolveAllocationSubnet(ctx, d.Client, &impNet)
	if err != nil {
		return nil, fmt.Errorf("resolve allocation subnet: %w", err)
	}

	_, cidr, err := gonet.ParseCIDR(allocSubnet)
	if err != nil {
		return nil, fmt.Errorf("parse subnet %q: %w", allocSubnet, err)
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
	ip, err := d.Alloc.Allocate(netKey, allocSubnet, gateway)
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
		if natErr := d.Net.EnsureNAT(ctx, allocSubnet, impNet.Spec.NAT.EgressInterface); natErr != nil {
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
		Subnet:          allocSubnet,
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
// Called in a goroutine after the VM reaches Running. probes may be nil — when
// nil, runProbes keeps the VSOCK connection open for metrics until ctx is done.
// vmNamespace and vmName identify the ImpVM to patch conditions onto.
func (d *FirecrackerDriver) runProbes(ctx context.Context, probes *impdevv1alpha1.ProbeSpec, vsockPath, vmNamespace, vmName, className string) {
	conn, err := agentvsock.Dial(ctx, vsockPath, 10000)
	if err != nil {
		logf.Log.Error(err, "runProbes: VSOCK dial failed", "vsock", vsockPath)
		return
	}
	defer conn.Close() //nolint:errcheck
	client := pb.NewGuestAgentClient(conn)

	// Always poll metrics when collector is set.
	if d.Metrics != nil {
		go d.pollMetrics(ctx, client, vmNamespace+"/"+vmName, className)
	}

	if probes == nil {
		<-ctx.Done() // keep connection open for metrics until VM stops
		return
	}

	runner := probe.NewRunner(client, probes, func(conds []metav1.Condition) {
		if d.Client == nil {
			logf.Log.Error(nil, "probe patcher: client is nil, skipping condition patch", "vm", vmNamespace+"/"+vmName)
			return
		}
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

// pollMetrics calls the guest Metrics RPC on every metricsInterval tick and
// forwards results to the Metrics collector. Errors are logged at V(1) and
// skipped — the guest may be unavailable during startup or shutdown.
// Runs until ctx is cancelled.
func (d *FirecrackerDriver) pollMetrics(ctx context.Context, client pb.GuestAgentClient, vmKey, className string) {
	ticker := time.NewTicker(metricsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			resp, err := client.Metrics(ctx, &pb.MetricsRequest{})
			if err != nil {
				logf.Log.V(1).Info("pollMetrics: guest agent unavailable", "vm", vmKey, "err", err)
				continue
			}
			d.Metrics.SetGuestMetrics(vmKey, d.NodeName, className,
				resp.CpuUsageRatio, resp.CpuIowaitRatio,
				resp.MemoryUsedBytes, resp.DiskUsedBytes)
		}
	}
}

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

	if proc.machine == nil {
		return SnapshotResult{}, fmt.Errorf("cannot snapshot VM %s: no machine handle (started by a previous agent process)", key)
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

	fullSnapshotOpt := firecracker.CreateSnapshotOpt(func(p *fcclientops.CreateSnapshotParams) {
		if p.Body != nil {
			p.Body.SnapshotType = models.SnapshotCreateParamsSnapshotTypeFull
		}
	})
	if err := proc.machine.CreateSnapshot(ctx, memPath, statePath, fullSnapshotOpt); err != nil {
		return SnapshotResult{}, fmt.Errorf("CreateSnapshot: %w", err)
	}

	log.Info("snapshot captured", "statePath", statePath, "memPath", memPath)
	return SnapshotResult{StatePath: statePath, MemPath: memPath}, nil
}

// applySnapshotBoot looks up the ImpVMSnapshot named by vm.Spec.SnapshotRef,
// reads its node-local SnapshotPath, and configures cfg.Snapshot for
// snapshot-based boot. The Firecracker SDK activates LoadSnapshotHandler
// automatically when cfg.hasSnapshot() returns true (i.e. SnapshotPath != "").
// If the snapshot has no node-local path yet (e.g. still being created), the
// function returns nil without modifying cfg — the VM will do a cold boot.
func (d *FirecrackerDriver) applySnapshotBoot(ctx context.Context, vm *impdevv1alpha1.ImpVM, cfg *firecracker.Config) error {
	log := logf.FromContext(ctx)
	snap := &impdevv1alpha1.ImpVMSnapshot{}
	if err := d.Client.Get(ctx, ctrlclient.ObjectKey{
		Namespace: vm.Namespace,
		Name:      vm.Spec.SnapshotRef,
	}, snap); err != nil {
		return fmt.Errorf("get snapshot %q: %w", vm.Spec.SnapshotRef, err)
	}
	if snap.Status.SnapshotPath == "" {
		log.Info("snapshot has no node-local path, skipping snapshot boot", "snapshot", snap.Name)
		return nil
	}
	cfg.Snapshot = firecracker.SnapshotConfig{
		SnapshotPath: filepath.Join(snap.Status.SnapshotPath, "vm.state"),
		MemFilePath:  filepath.Join(snap.Status.SnapshotPath, "vm.mem"),
		ResumeVM:     true,
	}
	log.Info("configured snapshot-based boot", "snapshotPath", cfg.Snapshot.SnapshotPath)
	return nil
}

// IsAlive reports whether the process with the given PID is still running.
// Uses kill(pid, 0) — succeeds if the process exists (even if zombie).
func (d *FirecrackerDriver) IsAlive(pid int64) bool {
	p, err := os.FindProcess(int(pid))
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// Reattach re-registers an already-running Firecracker VM into the driver's
// procs map without launching a new process. Called after an agent restart when
// the Firecracker process survived but the in-memory procs map was lost.
// Returns an error if the VM's API socket is not present on disk (which would
// mean the PID has been reused by a different process).
func (d *FirecrackerDriver) Reattach(_ context.Context, vm *impdevv1alpha1.ImpVM) error {
	sock := d.socketPath(vm)
	if _, err := os.Stat(sock); err != nil {
		return fmt.Errorf("reattach %s: socket %s not found — PID may be reused: %w",
			vmKey(vm), sock, err)
	}
	d.mu.Lock()
	// Note: the reattached fcProc has nil netInfo and machine.
	// If the process later dies and Inspect cleans it up, the allocator
	// entry will not be released via Stop (which checks netInfo). The
	// reconciler's handleRunning will detect the dead PID via IsAlive=false
	// on the next reconcile and call finishFailed, which does not release
	// the allocator either. This is an accepted limitation: the IP remains
	// reserved until the agent restarts again and the VM is definitively
	// dead (PID gone and socket absent).
	d.procs[vmKey(vm)] = &fcProc{
		pid:    vm.Status.RuntimePID,
		socket: sock,
	}
	d.mu.Unlock()
	return nil
}

// GetVSockPath returns the VSOCK Unix socket proxy path for the given VM key
// ("namespace/name"). The second return value is false when the VM is not
// running on this node or was started without the guest agent.
func (d *FirecrackerDriver) GetVSockPath(key string) (string, bool) {
	d.mu.Lock()
	proc, ok := d.procs[key]
	d.mu.Unlock()
	if !ok || proc.vsockPath == "" {
		return "", false
	}
	return proc.vsockPath, true
}
