//go:build linux

package agent

import (
	"os"
	"os/exec"
	"testing"
)

// hasFirecrackerBin returns true if the firecracker binary is available.
func hasFirecrackerBin() bool {
	_, err := exec.LookPath("firecracker")
	return err == nil || os.Getenv("FC_BIN") != ""
}

// hasKVM returns true if /dev/kvm is accessible.
func hasKVM() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

func TestFirecrackerDriverPlaceholder(t *testing.T) {
	t.Log("FirecrackerDriver test file compiles correctly")
}
