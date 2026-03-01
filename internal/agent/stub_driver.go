package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// pidCounter generates monotonically increasing fake PIDs.
var pidCounter int64

type stubVM struct {
	pid int64
	ip  string
}

// StubDriver is a VMDriver for testing and CI.
// It simulates VM start/stop/inspect without any real processes.
// Safe for concurrent use.
type StubDriver struct {
	mu         sync.Mutex
	states     map[string]*stubVM // keyed by "namespace/name"
	startErr   error
	stopErr    error
	inspectErr error
}

// InjectStartError causes the next Start call to return err (one-shot).
func (d *StubDriver) InjectStartError(err error) { d.mu.Lock(); d.startErr = err; d.mu.Unlock() }

// InjectStopError causes the next Stop call to return err (one-shot).
func (d *StubDriver) InjectStopError(err error) { d.mu.Lock(); d.stopErr = err; d.mu.Unlock() }

// InjectInspectError causes the next Inspect call to return err (one-shot).
func (d *StubDriver) InjectInspectError(err error) { d.mu.Lock(); d.inspectErr = err; d.mu.Unlock() }

// NewStubDriver creates a new StubDriver with empty state.
func NewStubDriver() *StubDriver {
	return &StubDriver{states: make(map[string]*stubVM)}
}

func vmKey(vm *impdevv1alpha1.ImpVM) string {
	return fmt.Sprintf("%s/%s", vm.Namespace, vm.Name)
}

// Start allocates a fake PID and IP and marks the VM as running immediately.
func (d *StubDriver) Start(_ context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.startErr != nil {
		err := d.startErr
		d.startErr = nil
		return 0, err
	}
	pid := atomic.AddInt64(&pidCounter, 1) + 10000
	ip := fmt.Sprintf("192.168.100.%d", pid%254+1)
	d.states[vmKey(vm)] = &stubVM{pid: pid, ip: ip}
	return pid, nil
}

// Stop removes the VM's entry.
func (d *StubDriver) Stop(_ context.Context, vm *impdevv1alpha1.ImpVM) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopErr != nil {
		err := d.stopErr
		d.stopErr = nil
		return err
	}
	delete(d.states, vmKey(vm))
	return nil
}

// Inspect returns the current state. Returns running=false if not started or already stopped.
func (d *StubDriver) Inspect(_ context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inspectErr != nil {
		err := d.inspectErr
		d.inspectErr = nil
		return VMState{}, err
	}
	s, ok := d.states[vmKey(vm)]
	if !ok {
		return VMState{Running: false}, nil
	}
	return VMState{Running: true, IP: s.ip, PID: s.pid}, nil
}
