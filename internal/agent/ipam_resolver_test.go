package agent

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestResolveAllocationSubnet_DefaultsToImpSubnet(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	impNet := &impdevv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net1", Namespace: "default"},
		Spec: impdevv1alpha1.ImpNetworkSpec{
			Subnet: "10.44.0.0/24",
		},
	}

	got, err := resolveAllocationSubnet(context.Background(), c, impNet)
	if err != nil {
		t.Fatalf("resolveAllocationSubnet: %v", err)
	}
	if got != "10.44.0.0/24" {
		t.Fatalf("got %q, want %q", got, "10.44.0.0/24")
	}
}

func TestResolveAllocationSubnet_UsesCiliumPoolCIDR(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(ciliumPoolGVK("v2alpha1"))
	pool.SetName("pool-a")
	pool.Object["spec"] = map[string]any{
		"cidrs": []any{
			map[string]any{"cidr": "10.77.0.0/24"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	impNet := &impdevv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net1", Namespace: "default"},
		Spec: impdevv1alpha1.ImpNetworkSpec{
			Subnet: "10.44.0.0/24",
			IPAM: &impdevv1alpha1.IPAMSpec{
				Provider: "cilium",
				Cilium:   &impdevv1alpha1.CiliumIPAMSpec{PoolRef: "pool-a"},
			},
		},
	}

	got, err := resolveAllocationSubnet(context.Background(), c, impNet)
	if err != nil {
		t.Fatalf("resolveAllocationSubnet: %v", err)
	}
	if got != "10.77.0.0/24" {
		t.Fatalf("got %q, want %q", got, "10.77.0.0/24")
	}
}

func TestResolveAllocationSubnet_CiliumPoolMissingReturnsError(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	impNet := &impdevv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net1", Namespace: "default"},
		Spec: impdevv1alpha1.ImpNetworkSpec{
			Subnet: "10.44.0.0/24",
			IPAM: &impdevv1alpha1.IPAMSpec{
				Provider: "cilium",
				Cilium:   &impdevv1alpha1.CiliumIPAMSpec{PoolRef: "missing-pool"},
			},
		},
	}

	_, err := resolveAllocationSubnet(context.Background(), c, impNet)
	if err == nil {
		t.Fatal("expected error for missing CiliumPodIPPool")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ciliumpodippool") {
		t.Fatalf("error = %v, want CiliumPodIPPool-related error", err)
	}
}
