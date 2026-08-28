// Package noderuntime adapts Imp's Firecracker implementation to the persistent
// node-runtime protocol.
package noderuntime

import (
	"context"
	"errors"
	"fmt"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
	"github.com/syscode-labs/imp/internal/agent/network"
	"github.com/syscode-labs/imp/internal/runtimeapi"
)

type driver interface {
	Start(context.Context, *impdevv1alpha1.ImpVM) (int64, error)
	Stop(context.Context, *impdevv1alpha1.ImpVM) error
	Inspect(context.Context, *impdevv1alpha1.ImpVM) (agent.VMState, error)
	Snapshot(context.Context, *impdevv1alpha1.ImpVM, string) (agent.SnapshotResult, error)
	Reattach(context.Context, *impdevv1alpha1.ImpVM) error
	IsAlive(int64) bool
	GetVSockPath(string) (string, bool)
}

// Backend delegates lifecycle and networking to the runtime-owned driver.
type Backend struct {
	Driver driver
	Net    network.NetManager
	// StatePath stores VM inventory that survives a runtime process restart.
	StatePath string
}

var _ runtimeapi.Backend = (*Backend)(nil)

func (b *Backend) Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	pid, err := b.Driver.Start(ctx, vm)
	if err != nil {
		return 0, err
	}
	state, err := b.Driver.Inspect(ctx, vm)
	if err != nil {
		return 0, fmt.Errorf("inspect started VM: %w", err)
	}
	if err := b.inventory().save(inventoryRecord{
		UID: vm.UID, Namespace: vm.Namespace, Name: vm.Name, PID: pid, IP: state.IP,
	}); err != nil {
		return 0, fmt.Errorf("persist started VM: %w", err)
	}
	return pid, nil
}
func (b *Backend) Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	if err := b.Driver.Stop(ctx, vm); err != nil {
		return err
	}
	if err := b.inventory().remove(vm.UID); err != nil {
		return fmt.Errorf("remove stopped VM inventory: %w", err)
	}
	return nil
}
func (b *Backend) Get(ctx context.Context, vm *impdevv1alpha1.ImpVM) (runtimeapi.VMState, error) {
	state, err := b.Driver.Inspect(ctx, vm)
	if err != nil || state.Running {
		return runtimeapi.VMState{Running: state.Running, IP: state.IP, PID: state.PID}, err
	}
	record, err := b.inventory().load(vm.UID)
	if errors.Is(err, errInventoryNotFound) {
		return runtimeapi.VMState{}, nil
	}
	if err != nil {
		return runtimeapi.VMState{}, fmt.Errorf("load VM inventory: %w", err)
	}
	if record.UID != vm.UID || record.Namespace != vm.Namespace || record.Name != vm.Name || record.PID != vm.Status.RuntimePID || !b.Driver.IsAlive(record.PID) {
		return runtimeapi.VMState{}, nil
	}
	if err := b.Driver.Reattach(ctx, vm); err != nil {
		return runtimeapi.VMState{}, fmt.Errorf("reattach VM: %w", err)
	}
	return runtimeapi.VMState{Running: true, IP: record.IP, PID: record.PID}, nil
}
func (b *Backend) Snapshot(ctx context.Context, vm *impdevv1alpha1.ImpVM, destDir string) (runtimeapi.SnapshotResult, error) {
	result, err := b.Driver.Snapshot(ctx, vm, destDir)
	return runtimeapi.SnapshotResult{StatePath: result.StatePath, MemPath: result.MemPath}, err
}
func (b *Backend) Reattach(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	return b.Driver.Reattach(ctx, vm)
}
func (b *Backend) IsAlive(pid int64) bool                 { return b.Driver.IsAlive(pid) }
func (b *Backend) GetVSockPath(key string) (string, bool) { return b.Driver.GetVSockPath(key) }
func (b *Backend) EnsureNetwork(ctx context.Context, bridgeName, gatewayIP string, prefixLen int) error {
	return b.Net.EnsureNetwork(ctx, bridgeName, gatewayIP, prefixLen)
}
func (b *Backend) SetupVM(ctx context.Context, tapName, bridgeName, macAddr string) error {
	return b.Net.SetupVM(ctx, tapName, bridgeName, macAddr)
}
func (b *Backend) TeardownVM(ctx context.Context, tapName string) error {
	return b.Net.TeardownVM(ctx, tapName)
}
func (b *Backend) EnsureNAT(ctx context.Context, subnet, egressInterface string) error {
	return b.Net.EnsureNAT(ctx, subnet, egressInterface)
}
func (b *Backend) RemoveNAT(ctx context.Context, subnet, egressInterface string) error {
	return b.Net.RemoveNAT(ctx, subnet, egressInterface)
}
func (b *Backend) EnsureEgressDeny(ctx context.Context, subnet string, denyCIDRs []string) error {
	return b.Net.EnsureEgressDeny(ctx, subnet, denyCIDRs)
}
func (b *Backend) RemoveEgressDeny(ctx context.Context, subnet string) error {
	return b.Net.RemoveEgressDeny(ctx, subnet)
}
func (b *Backend) EnsureVXLAN(ctx context.Context, vni uint32, ifaceName, nodeIP, bridgeName string) error {
	return b.Net.EnsureVXLAN(ctx, vni, ifaceName, nodeIP, bridgeName)
}
func (b *Backend) SyncFDB(ctx context.Context, ifaceName string, entries []network.FDBEntry) error {
	return b.Net.SyncFDB(ctx, ifaceName, entries)
}
