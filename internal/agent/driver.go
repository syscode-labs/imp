package agent

import (
	"context"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// VMState is the runtime state of a VM as reported by a VMDriver.
type VMState struct {
	// Running is true if the VM process is alive.
	Running bool
	// IP is the IP address assigned to the VM. Empty until the VM is running.
	IP string
	// PID is the process ID of the VM runtime on this node.
	PID int64
}

// SnapshotResult holds the paths of the two files produced by a Firecracker snapshot.
type SnapshotResult struct {
	// StatePath is the path to the VM state file (CPU registers, device state).
	StatePath string
	// MemPath is the path to the memory dump file.
	MemPath string
}

// VMDriver abstracts the VM runtime backend.
// Implementations: StubDriver (testing/CI), FirecrackerDriver (production, Phase 1).
type VMDriver interface {
	// Start boots the VM and returns its runtime PID.
	Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (pid int64, err error)

	// Stop halts the VM. Blocks until stopped or ctx is cancelled.
	Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error

	// Inspect returns the current runtime state of the VM.
	// Called every reconcile to detect unexpected exits.
	Inspect(ctx context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error)

	// Snapshot pauses the VM, writes state+memory files to destDir, then resumes it.
	// The VM is always resumed before Snapshot returns, even on error.
	// destDir must exist and be writable.
	Snapshot(ctx context.Context, vm *impdevv1alpha1.ImpVM, destDir string) (SnapshotResult, error)

	// IsAlive returns true if the process with the given PID is still running.
	// Uses syscall.Kill(pid, 0) on Linux. StubDriver returns IsAliveResult.
	IsAlive(pid int64) bool

	// Reattach re-registers an already-running VM (started by a previous agent
	// process) into the driver's internal state without launching a new process.
	// Called during lazy recovery when the agent restarts and finds a live PID.
	Reattach(ctx context.Context, vm *impdevv1alpha1.ImpVM) error
}
