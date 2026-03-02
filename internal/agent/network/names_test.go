package network_test

import (
	"regexp"
	"testing"

	"github.com/syscode-labs/imp/internal/agent/network"
)

func TestBridgeName_length(t *testing.T) {
	for _, key := range []string{"default/my-net", "prod/network-with-long-name", ""} {
		got := network.BridgeName(key)
		if len(got) > 15 {
			t.Errorf("BridgeName(%q) = %q (len %d), exceeds 15", key, got, len(got))
		}
		if len(got) == 0 {
			t.Errorf("BridgeName(%q) returned empty string", key)
		}
	}
}

func TestBridgeName_deterministic(t *testing.T) {
	if network.BridgeName("default/net") != network.BridgeName("default/net") {
		t.Error("BridgeName is not deterministic")
	}
}

func TestBridgeName_distinct(t *testing.T) {
	if network.BridgeName("a/net1") == network.BridgeName("a/net2") {
		t.Error("different keys should produce different names (hash collision for test inputs)")
	}
}

func TestTAPName_length(t *testing.T) {
	for _, key := range []string{"default/my-vm", "prod/vm-with-long-name", ""} {
		got := network.TAPName(key)
		if len(got) > 15 {
			t.Errorf("TAPName(%q) = %q (len %d), exceeds 15", key, got, len(got))
		}
	}
}

func TestMACAddr_format(t *testing.T) {
	re := regexp.MustCompile(`^02:[0-9a-f]{2}(:[0-9a-f]{2}){4}$`)
	for _, key := range []string{"default/vm1", "ns/vm2"} {
		got := network.MACAddr(key)
		if !re.MatchString(got) {
			t.Errorf("MACAddr(%q) = %q, want 02:xx:xx:xx:xx:xx", key, got)
		}
	}
}
