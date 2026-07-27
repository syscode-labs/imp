//go:build linux

package agent

import (
	"fmt"
	"net/http"

	"github.com/syscode-labs/imp/internal/capability"
)

// CapabilityReadyzCheck returns a manager readyz check that reports ready only
// while the host passes the KVM and Firecracker capability probe. binPath is
// the configured Firecracker binary path (empty resolves it via PATH).
func CapabilityReadyzCheck(binPath string) func(*http.Request) error {
	return func(*http.Request) error {
		r := capability.CheckDefault(binPath)
		if !r.OK() {
			return fmt.Errorf("kvm capability check failed: kvm_available=%v (%s) firecracker_available=%v (%s)",
				r.KVMAvailable, r.KVMError, r.FirecrackerAvailable, r.FirecrackerError)
		}
		return nil
	}
}
