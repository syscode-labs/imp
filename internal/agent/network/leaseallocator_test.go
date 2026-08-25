package network_test

import (
	"context"
	"testing"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/syscode-labs/imp/internal/agent/network"
)

func TestLeaseAllocator_coordinatesClaimsAcrossAllocators(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add coordination scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	first := network.NewLeaseAllocator(client)
	second := network.NewLeaseAllocator(client)

	firstIP, err := first.Allocate(context.Background(), "default/network", "10.0.0.0/30", "10.0.0.1", "default/first")
	if err != nil {
		t.Fatalf("first allocation: %v", err)
	}
	if firstIP != "10.0.0.2" {
		t.Fatalf("first allocation = %q, want 10.0.0.2", firstIP)
	}

	if _, err := second.Allocate(context.Background(), "default/network", "10.0.0.0/30", "10.0.0.1", "default/second"); err == nil {
		t.Fatal("second allocator claimed an IP already held by another allocator")
	}
}

func TestLeaseAllocator_releaseAllowsReuseByAnotherHolder(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add coordination scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	allocator := network.NewLeaseAllocator(client)

	ip, err := allocator.Allocate(context.Background(), "default/network", "10.0.0.0/30", "10.0.0.1", "default/first")
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if err := allocator.Release(context.Background(), "default/network", ip, "default/first"); err != nil {
		t.Fatalf("release: %v", err)
	}

	reused, err := allocator.Allocate(context.Background(), "default/network", "10.0.0.0/30", "10.0.0.1", "default/second")
	if err != nil {
		t.Fatalf("allocate released IP: %v", err)
	}
	if reused != ip {
		t.Errorf("reused IP = %q, want %q", reused, ip)
	}
}

func TestLeaseAllocator_retryReturnsExistingHolderClaim(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add coordination scheme: %v", err)
	}
	allocator := network.NewLeaseAllocator(fake.NewClientBuilder().WithScheme(scheme).Build())

	first, err := allocator.Allocate(context.Background(), "default/network", "10.0.0.0/29", "10.0.0.1", "default/vm")
	if err != nil {
		t.Fatalf("first allocation: %v", err)
	}
	retry, err := allocator.Allocate(context.Background(), "default/network", "10.0.0.0/29", "10.0.0.1", "default/vm")
	if err != nil {
		t.Fatalf("retry allocation: %v", err)
	}
	if retry != first {
		t.Fatalf("retry allocation = %q, want existing claim %q", retry, first)
	}
}

func TestLeaseAllocator_doesNotReleaseAnotherHoldersClaim(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add coordination scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	allocator := network.NewLeaseAllocator(client)

	ip, err := allocator.Allocate(context.Background(), "default/network", "10.0.0.0/30", "10.0.0.1", "default/first")
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if err := allocator.Release(context.Background(), "default/network", ip, "default/second"); err == nil {
		t.Fatal("release by another holder succeeded")
	}
}
