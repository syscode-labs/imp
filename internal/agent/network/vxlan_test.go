package network_test

import (
	"testing"

	"github.com/syscode-labs/imp/internal/agent/network"
)

func TestVXLANParams_deterministic(t *testing.T) {
	uid := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	vni1, iface1 := network.VXLANParams(uid)
	vni2, iface2 := network.VXLANParams(uid)
	if vni1 != vni2 {
		t.Errorf("VXLANParams VNI not deterministic: %d != %d", vni1, vni2)
	}
	if iface1 != iface2 {
		t.Errorf("VXLANParams ifaceName not deterministic: %q != %q", iface1, iface2)
	}
}

func TestVXLANParams_vniNeverZero(t *testing.T) {
	// Test many UIDs to ensure VNI is never 0.
	uids := []string{
		"00000000-0000-0000-0000-000000000000",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
		"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"deadbeef-dead-beef-dead-beefdeadbeef",
		"",
	}
	for _, uid := range uids {
		vni, _ := network.VXLANParams(uid)
		if vni == 0 {
			t.Errorf("VXLANParams(%q) returned VNI 0", uid)
		}
		if vni > 0xFFFFFF {
			t.Errorf("VXLANParams(%q) returned VNI %d > 16777215", uid, vni)
		}
	}
}

func TestVXLANParams_ifaceNameLength(t *testing.T) {
	uids := []string{
		"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"short",
		"",
		"00000000-0000-0000-0000-000000000000",
	}
	for _, uid := range uids {
		_, iface := network.VXLANParams(uid)
		if len(iface) > 15 {
			t.Errorf("VXLANParams(%q) ifaceName %q has length %d > 15", uid, iface, len(iface))
		}
		if len(iface) == 0 {
			t.Errorf("VXLANParams(%q) returned empty ifaceName", uid)
		}
	}
}

func TestVXLANParams_distinct(t *testing.T) {
	_, iface1 := network.VXLANParams("uid-aaa")
	_, iface2 := network.VXLANParams("uid-bbb")
	if iface1 == iface2 {
		t.Errorf("different UIDs produced same ifaceName %q", iface1)
	}
}
