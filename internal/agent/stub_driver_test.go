package agent_test

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
)

func TestStubDriver_StartInspectStop(t *testing.T) {
	ctx := context.Background()
	d := agent.NewStubDriver()

	vm := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vm", Namespace: "default"},
	}

	// Before Start: Inspect returns not-running.
	state, err := d.Inspect(ctx, vm)
	if err != nil {
		t.Fatalf("Inspect before Start: %v", err)
	}
	if state.Running {
		t.Fatal("expected not running before Start")
	}

	// Start allocates a PID and IP.
	pid, err := d.Start(ctx, vm)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}

	// Inspect after Start: running=true, IP set, PID matches.
	state, err = d.Inspect(ctx, vm)
	if err != nil {
		t.Fatalf("Inspect after Start: %v", err)
	}
	if !state.Running {
		t.Fatal("expected running after Start")
	}
	if state.IP == "" {
		t.Fatal("expected non-empty IP after Start")
	}
	if state.PID != pid {
		t.Fatalf("expected PID %d, got %d", pid, state.PID)
	}

	// Stop clears state.
	if err := d.Stop(ctx, vm); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Inspect after Stop: not running.
	state, err = d.Inspect(ctx, vm)
	if err != nil {
		t.Fatalf("Inspect after Stop: %v", err)
	}
	if state.Running {
		t.Fatal("expected not running after Stop")
	}
}

func TestStubDriver_Snapshot_success(t *testing.T) {
	d := agent.NewStubDriver()
	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace, vm.Name = "ns", "vm1"
	_, _ = d.Start(context.Background(), vm)

	res, err := d.Snapshot(context.Background(), vm, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatePath == "" || res.MemPath == "" {
		t.Errorf("expected non-empty paths, got %+v", res)
	}
}

func TestStubDriver_Snapshot_notRunning(t *testing.T) {
	d := agent.NewStubDriver()
	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace, vm.Name = "ns", "vm-missing"

	_, err := d.Snapshot(context.Background(), vm, t.TempDir())
	if err == nil {
		t.Error("expected error for non-running VM")
	}
}

func TestStubDriver_ConcurrentSafe(t *testing.T) {
	ctx := context.Background()
	d := agent.NewStubDriver()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			vm := &impdevv1alpha1.ImpVM{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("vm-%d", i),
					Namespace: "default",
				},
			}
			_, _ = d.Start(ctx, vm)
			_, _ = d.Inspect(ctx, vm)
			_ = d.Stop(ctx, vm)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
