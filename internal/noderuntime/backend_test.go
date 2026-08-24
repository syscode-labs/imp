package noderuntime

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
)

func TestBackendStartPersistsRuntimeInventory(t *testing.T) {
	t.Parallel()

	vm := testVM()
	driver := &fakeDriver{startPID: 42, state: agent.VMState{Running: true, PID: 42, IP: "10.0.0.2"}}
	backend := &Backend{Driver: driver, StatePath: t.TempDir()}

	pid, err := backend.Start(context.Background(), vm)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if pid != 42 {
		t.Fatalf("Start() PID = %d, want 42", pid)
	}

	record, err := backend.inventory().load(vm.UID)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if record.UID != vm.UID || record.Namespace != vm.Namespace || record.Name != vm.Name || record.PID != 42 || record.IP != "10.0.0.2" {
		t.Fatalf("inventory record = %#v, want VM identity, PID, and IP", record)
	}
}

func TestBackendStopRemovesRuntimeInventoryAfterSuccessfulStop(t *testing.T) {
	t.Parallel()

	vm := testVM()
	driver := &fakeDriver{startPID: 42, state: agent.VMState{Running: true, PID: 42, IP: "10.0.0.2"}}
	backend := &Backend{Driver: driver, StatePath: t.TempDir()}
	if _, err := backend.Start(context.Background(), vm); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := backend.Stop(context.Background(), vm); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := backend.inventory().load(vm.UID); !errors.Is(err, errInventoryNotFound) {
		t.Fatalf("load() error = %v, want inventory removed", err)
	}
}

func TestBackendStopRetainsRuntimeInventoryAfterFailedStop(t *testing.T) {
	t.Parallel()

	vm := testVM()
	driver := &fakeDriver{startPID: 42, state: agent.VMState{Running: true, PID: 42, IP: "10.0.0.2"}, stopErr: errors.New("stop failed")}
	backend := &Backend{Driver: driver, StatePath: t.TempDir()}
	if _, err := backend.Start(context.Background(), vm); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := backend.Stop(context.Background(), vm); err == nil {
		t.Fatal("Stop() error = nil, want error")
	}
	if _, err := backend.inventory().load(vm.UID); err != nil {
		t.Fatalf("load() error = %v, want inventory retained", err)
	}
}

func TestBackendGetReattachesLiveVMFromRuntimeInventory(t *testing.T) {
	t.Parallel()

	vm := testVM()
	vm.Status.RuntimePID = 42
	statePath := t.TempDir()
	starter := &Backend{Driver: &fakeDriver{startPID: 42, state: agent.VMState{Running: true, PID: 42, IP: "10.0.0.2"}}, StatePath: statePath}
	if _, err := starter.Start(context.Background(), vm); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	restartedDriver := &fakeDriver{alive: true}
	restarted := &Backend{Driver: restartedDriver, StatePath: statePath}

	state, err := restarted.Get(context.Background(), vm)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !state.Running || state.PID != 42 || state.IP != "10.0.0.2" {
		t.Fatalf("Get() = %#v, want recovered runtime state", state)
	}
	if restartedDriver.reattachCalls != 1 {
		t.Fatalf("Reattach() calls = %d, want 1", restartedDriver.reattachCalls)
	}
}

func TestBackendGetDoesNotReattachWhenStatusPIDDoesNotMatchInventory(t *testing.T) {
	t.Parallel()

	vm := testVM()
	statePath := t.TempDir()
	starter := &Backend{Driver: &fakeDriver{startPID: 42, state: agent.VMState{Running: true, PID: 42, IP: "10.0.0.2"}}, StatePath: statePath}
	if _, err := starter.Start(context.Background(), vm); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	vm.Status.RuntimePID = 99
	restartedDriver := &fakeDriver{alive: true}

	state, err := (&Backend{Driver: restartedDriver, StatePath: statePath}).Get(context.Background(), vm)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state.Running {
		t.Fatalf("Get() = %#v, want not running", state)
	}
	if restartedDriver.reattachCalls != 0 {
		t.Fatalf("Reattach() calls = %d, want 0", restartedDriver.reattachCalls)
	}
}

func testVM() *impdevv1alpha1.ImpVM {
	return &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "worker", UID: types.UID("vm-uid")},
	}
}

type fakeDriver struct {
	startPID int64
	state    agent.VMState
	stopErr  error
	alive    bool

	reattachCalls int
}

func (d *fakeDriver) Start(context.Context, *impdevv1alpha1.ImpVM) (int64, error) {
	return d.startPID, nil
}
func (d *fakeDriver) Stop(context.Context, *impdevv1alpha1.ImpVM) error { return d.stopErr }
func (d *fakeDriver) Inspect(context.Context, *impdevv1alpha1.ImpVM) (agent.VMState, error) {
	return d.state, nil
}
func (d *fakeDriver) Reattach(context.Context, *impdevv1alpha1.ImpVM) error {
	d.reattachCalls++
	return nil
}
func (d *fakeDriver) IsAlive(int64) bool { return d.alive }
func (d *fakeDriver) Snapshot(context.Context, *impdevv1alpha1.ImpVM, string) (agent.SnapshotResult, error) {
	return agent.SnapshotResult{}, nil
}
func (d *fakeDriver) GetVSockPath(string) (string, bool) { return "", false }
