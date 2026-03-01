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
	mu     sync.Mutex
	states map[string]*stubVM // keyed by "namespace/name"
}

// NewStubDriver creates a new StubDriver with empty state.
func NewStubDriver() *StubDriver {
	return &StubDriver{states: make(map[string]*stubVM)}
}

func vmKey(vm *impdevv1alpha1.ImpVM) string {
	return fmt.Sprintf("%s/%s", vm.Namespace, vm.Name)
}

// Start allocates a fake PID and IP and marks the VM as running immediately.
func (d *StubDriver) Start(_ context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	pid := atomic.AddInt64(&pidCounter, 1) + 10000
	ip := fmt.Sprintf("192.168.100.%d", pid%254+1)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.states[vmKey(vm)] = &stubVM{pid: pid, ip: ip}
	return pid, nil
}

// Stop removes the VM's entry.
func (d *StubDriver) Stop(_ context.Context, vm *impdevv1alpha1.ImpVM) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.states, vmKey(vm))
	return nil
}

// Inspect returns the current state. Returns running=false if not started or already stopped.
func (d *StubDriver) Inspect(_ context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.states[vmKey(vm)]
	if !ok {
		return VMState{Running: false}, nil
	}
	return VMState{Running: true, IP: s.ip, PID: s.pid}, nil
}
