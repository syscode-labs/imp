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

func TestFindNftHandle(t *testing.T) {
	output := `table ip imp_nat {
	chain postrouting {
		type nat hook postrouting priority srcnat; policy accept;
		ip saddr 10.0.1.0/24 oifname "eth0" masquerade # handle 3
		ip saddr 10.0.2.0/24 oifname "eth0" masquerade # handle 7
	}
}`
	tests := []struct {
		subnet string
		want   string
	}{
		{"10.0.1.0/24", "3"},
		{"10.0.2.0/24", "7"},
		{"10.0.3.0/24", ""},
	}
	for _, tc := range tests {
		got := network.FindNftHandle(output, tc.subnet)
		if got != tc.want {
			t.Errorf("FindNftHandle(_, %q) = %q, want %q", tc.subnet, got, tc.want)
		}
	}
}
