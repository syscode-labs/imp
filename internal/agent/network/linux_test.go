//go:build linux

package network_test

import (
	"testing"

	"github.com/syscode-labs/imp/internal/agent/network"
)

func TestLinuxNetManager_implementsInterface(t *testing.T) {
	// compile-time interface check
	var _ network.NetManager = (*network.LinuxNetManager)(nil)
}

func TestLinuxNetManager_newDoesNotPanic(t *testing.T) {
	mgr := network.NewLinuxNetManager()
	if mgr == nil {
		t.Fatal("NewLinuxNetManager returned nil")
	}
}
