package network_test

import (
	"testing"

	"github.com/syscode-labs/imp/internal/agent/network"
)

func TestAllocator_basic(t *testing.T) {
	a := network.NewAllocator()
	ip, err := a.Allocate("default/net", "192.168.100.0/24", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default gateway is .1; first allocatable IP is .2.
	if ip != "192.168.100.2" {
		t.Errorf("got %q, want %q", ip, "192.168.100.2")
	}
}

func TestAllocator_skipsGateway(t *testing.T) {
	a := network.NewAllocator()
	// Allocate with explicit gateway = .1 (same as default for this subnet).
	// First allocated IP should be .2.
	ip, err := a.Allocate("ns/net", "10.0.0.0/24", "10.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip == "10.0.0.1" {
		t.Error("allocated the gateway address")
	}
	if ip == "10.0.0.0" {
		t.Error("allocated the network address")
	}
}

func TestAllocator_sequentialAllocations(t *testing.T) {
	a := network.NewAllocator()
	ip1, _ := a.Allocate("ns/net", "10.1.0.0/24", "10.1.0.1")
	ip2, _ := a.Allocate("ns/net", "10.1.0.0/24", "10.1.0.1")
	if ip1 == ip2 {
		t.Errorf("expected distinct IPs, both got %q", ip1)
	}
}

func TestAllocator_release(t *testing.T) {
	a := network.NewAllocator()
	ip, _ := a.Allocate("ns/net", "10.2.0.0/24", "10.2.0.1")
	a.Release("ns/net", ip)
	ip2, err := a.Allocate("ns/net", "10.2.0.0/24", "10.2.0.1")
	if err != nil {
		t.Fatalf("unexpected error after release: %v", err)
	}
	if ip2 != ip {
		t.Errorf("expected reuse of released IP %q, got %q", ip, ip2)
	}
}

func TestAllocator_reserve(t *testing.T) {
	a := network.NewAllocator()
	a.Reserve("ns/net", "10.3.0.2")
	ip, _ := a.Allocate("ns/net", "10.3.0.0/24", "10.3.0.1")
	if ip == "10.3.0.2" {
		t.Error("reserved IP was re-allocated")
	}
}

func TestAllocator_exhaustion(t *testing.T) {
	a := network.NewAllocator()
	// /30 has network(.0), gateway(.1), one usable(.2), broadcast(.3)
	_, _ = a.Allocate("ns/net", "10.4.0.0/30", "10.4.0.1")
	_, err := a.Allocate("ns/net", "10.4.0.0/30", "10.4.0.1")
	if err == nil {
		t.Error("expected error when subnet exhausted")
	}
}

func TestAllocator_multipleNetworks(t *testing.T) {
	a := network.NewAllocator()
	ip1, _ := a.Allocate("ns/net1", "10.5.0.0/24", "")
	ip2, _ := a.Allocate("ns/net2", "10.5.0.0/24", "")
	// Both start at .2 since they have independent state
	if ip1 != ip2 {
		t.Errorf("different network keys should have independent allocation state, got %q and %q", ip1, ip2)
	}
}

func TestAllocator_release_wasLast_true(t *testing.T) {
	a := network.NewAllocator()
	ip, _ := a.Allocate("ns/net", "10.0.0.0/24", "")
	wasLast := a.Release("ns/net", ip)
	if !wasLast {
		t.Error("expected wasLast=true when last VM released")
	}
}

func TestAllocator_release_wasLast_false(t *testing.T) {
	a := network.NewAllocator()
	ip1, _ := a.Allocate("ns/net", "10.0.0.0/24", "")
	_, _ = a.Allocate("ns/net", "10.0.0.0/24", "")
	wasLast := a.Release("ns/net", ip1)
	if wasLast {
		t.Error("expected wasLast=false when one VM still allocated")
	}
}

func TestAllocator_release_unknown_key(t *testing.T) {
	a := network.NewAllocator()
	// Releasing an unknown key must not panic and must return true.
	wasLast := a.Release("ns/notexist", "10.0.0.2")
	if !wasLast {
		t.Error("expected wasLast=true for unknown key (no VMs remain)")
	}
}

func TestAllocator_reserve_idempotent(t *testing.T) {
	a := network.NewAllocator()
	// Calling Reserve twice for the same IP (e.g. two agent restarts) must not
	// inflate vmCount. A single Release should return wasLast=true.
	a.Reserve("ns/net", "10.0.0.2")
	a.Reserve("ns/net", "10.0.0.2") // duplicate — idempotent

	wasLast := a.Release("ns/net", "10.0.0.2")
	if !wasLast {
		t.Error("expected wasLast=true: duplicate Reserve must not inflate vmCount")
	}
}

func TestAllocator_reserve_countsForWasLast(t *testing.T) {
	a := network.NewAllocator()
	// Reserve two IPs as if recovering running VMs on restart.
	a.Reserve("ns/net", "10.0.0.2")
	a.Reserve("ns/net", "10.0.0.3")

	// Releasing the first should NOT be wasLast.
	wasLast := a.Release("ns/net", "10.0.0.2")
	if wasLast {
		t.Error("expected wasLast=false after releasing one of two reserved IPs")
	}

	// Releasing the second should be wasLast.
	wasLast = a.Release("ns/net", "10.0.0.3")
	if !wasLast {
		t.Error("expected wasLast=true after releasing last reserved IP")
	}
}
