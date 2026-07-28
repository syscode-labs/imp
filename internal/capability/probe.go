// Package capability probes whether a host can run Firecracker microVMs:
// a usable /dev/kvm device and an executable Firecracker binary.
package capability

import (
	"fmt"
	"os"
	"os/exec"
)

// DefaultKVMDevicePath is the standard KVM device node.
const DefaultKVMDevicePath = "/dev/kvm"

// Result reports whether a host passed the KVM and Firecracker capability probe.
type Result struct {
	KVMAvailable         bool
	KVMError             string
	FirecrackerAvailable bool
	FirecrackerError     string
}

// OK reports whether both the KVM and Firecracker checks passed.
func (r Result) OK() bool {
	return r.KVMAvailable && r.FirecrackerAvailable
}

// Check probes kvmPath for a usable KVM device and binPath for an executable
// Firecracker binary. If binPath is empty, the binary is resolved via PATH.
func Check(kvmPath, binPath string) Result {
	var r Result

	if f, err := os.OpenFile(kvmPath, os.O_RDWR, 0); err != nil { //nolint:gosec // G304: kvmPath is an operator-supplied device path, not user input
		r.KVMError = fmt.Sprintf("%s not available: %v", kvmPath, err)
	} else {
		_ = f.Close()
		r.KVMAvailable = true
	}

	resolved := binPath
	if resolved == "" {
		found, err := exec.LookPath("firecracker")
		if err != nil {
			r.FirecrackerError = fmt.Sprintf("firecracker binary not found: %v", err)
			return r
		}
		resolved = found
	}

	info, err := os.Stat(resolved)
	if err != nil {
		r.FirecrackerError = fmt.Sprintf("firecracker binary not found at %s: %v", resolved, err)
		return r
	}
	if info.IsDir() || info.Mode()&0111 == 0 {
		r.FirecrackerError = fmt.Sprintf("firecracker binary at %s is not executable", resolved)
		return r
	}
	r.FirecrackerAvailable = true
	return r
}

// CheckDefault probes the standard KVM device path and binPath.
func CheckDefault(binPath string) Result {
	return Check(DefaultKVMDevicePath, binPath)
}
