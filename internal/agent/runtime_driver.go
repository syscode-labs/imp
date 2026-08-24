//go:build linux

package agent

import (
	"context"
	"time"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/network"
	"github.com/syscode-labs/imp/internal/runtimeapi"
)

// RuntimeDriver is a VMDriver adapter for the persistent node runtime. It has
// no Firecracker process state; that state belongs to the runtime DaemonSet.
type RuntimeDriver struct {
	client *runtimeapi.Client
}

var _ VMDriver = (*RuntimeDriver)(nil)

// NewRuntimeDriver connects VM lifecycle requests to endpoint.
func NewRuntimeDriver(endpoint string) *RuntimeDriver {
	return &RuntimeDriver{client: runtimeapi.NewClient(endpoint)}
}

func (d *RuntimeDriver) Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	return d.client.Start(ctx, vm)
}

func (d *RuntimeDriver) Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	return d.client.Stop(ctx, vm)
}

func (d *RuntimeDriver) Inspect(ctx context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error) {
	state, err := d.client.Get(ctx, vm)
	return VMState{Running: state.Running, IP: state.IP, PID: state.PID}, err
}

func (d *RuntimeDriver) Snapshot(ctx context.Context, vm *impdevv1alpha1.ImpVM, destDir string) (SnapshotResult, error) {
	result, err := d.client.Snapshot(ctx, vm, destDir)
	return SnapshotResult{StatePath: result.StatePath, MemPath: result.MemPath}, err
}

func (d *RuntimeDriver) IsAlive(pid int64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	alive, err := d.client.IsAlive(ctx, pid)
	return err == nil && alive
}

func (d *RuntimeDriver) Reattach(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	return d.client.Reattach(ctx, vm)
}

// GetVSockPath implements the plugin API's VSOCK lookup seam.
func (d *RuntimeDriver) GetVSockPath(key string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	path, found, err := d.client.GetVSockPath(ctx, key)
	return path, err == nil && found
}

// RuntimeNetManager forwards all host-network mutations to the runtime. The
// agent still computes desired FDB state, but only the runtime touches host
// network resources.
type RuntimeNetManager struct {
	client *runtimeapi.Client
}

var _ network.NetManager = (*RuntimeNetManager)(nil)

// NewRuntimeNetManager returns a NetManager backed by endpoint.
func NewRuntimeNetManager(endpoint string) *RuntimeNetManager {
	return &RuntimeNetManager{client: runtimeapi.NewClient(endpoint)}
}

func (m *RuntimeNetManager) EnsureNetwork(ctx context.Context, bridgeName, gatewayIP string, prefixLen int) error {
	return m.client.EnsureNetwork(ctx, bridgeName, gatewayIP, prefixLen)
}
func (m *RuntimeNetManager) SetupVM(ctx context.Context, tapName, bridgeName, macAddr string) error {
	return m.client.SetupVM(ctx, tapName, bridgeName, macAddr)
}
func (m *RuntimeNetManager) TeardownVM(ctx context.Context, tapName string) error {
	return m.client.TeardownVM(ctx, tapName)
}
func (m *RuntimeNetManager) EnsureNAT(ctx context.Context, subnet, egressInterface string) error {
	return m.client.EnsureNAT(ctx, subnet, egressInterface)
}
func (m *RuntimeNetManager) RemoveNAT(ctx context.Context, subnet, egressInterface string) error {
	return m.client.RemoveNAT(ctx, subnet, egressInterface)
}
func (m *RuntimeNetManager) EnsureVXLAN(ctx context.Context, vni uint32, ifaceName, nodeIP, bridgeName string) error {
	return m.client.EnsureVXLAN(ctx, vni, ifaceName, nodeIP, bridgeName)
}
func (m *RuntimeNetManager) SyncFDB(ctx context.Context, ifaceName string, entries []network.FDBEntry) error {
	return m.client.SyncFDB(ctx, ifaceName, entries)
}
