package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = impdevv1alpha1.AddToScheme(s)
	return s
}

func TestReconcileCiliumPool_CreatesManagedPool(t *testing.T) {
	net := &impdevv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "test-net", Namespace: "default"},
		Spec: impdevv1alpha1.ImpNetworkSpec{
			Subnet: "10.100.0.0/24",
			IPAM: &impdevv1alpha1.IPAMSpec{
				Provider: "cilium",
				Cilium:   &impdevv1alpha1.CiliumIPAMSpec{PoolRef: "test-pool"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(net).
		Build()

	r := &ImpNetworkReconciler{Client: fakeClient, Scheme: testScheme()}
	err := r.reconcileCiliumPool(context.Background(), net)
	require.NoError(t, err)

	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "cilium.io", Version: "v2alpha1", Kind: "CiliumPodIPPool",
	})
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "test-pool"}, pool)
	require.NoError(t, err)

	cidrs, _, _ := unstructured.NestedSlice(pool.Object, "spec", "ipv4", "cidrs")
	require.Len(t, cidrs, 1)
	cidrMap, ok := cidrs[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "10.100.0.0/24", cidrMap["cidr"])
}

func TestReconcileCiliumPool_RespectsOverrideCidr(t *testing.T) {
	net := &impdevv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "test-net", Namespace: "default"},
		Spec: impdevv1alpha1.ImpNetworkSpec{
			Subnet: "10.100.0.0/24",
			IPAM: &impdevv1alpha1.IPAMSpec{
				Provider: "cilium",
				Cilium: &impdevv1alpha1.CiliumIPAMSpec{
					PoolRef: "test-pool",
					Cidr:    "10.200.0.0/24",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(net).
		Build()

	r := &ImpNetworkReconciler{Client: fakeClient, Scheme: testScheme()}
	require.NoError(t, r.reconcileCiliumPool(context.Background(), net))

	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "cilium.io", Version: "v2alpha1", Kind: "CiliumPodIPPool",
	})
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{Name: "test-pool"}, pool))

	cidrs, _, _ := unstructured.NestedSlice(pool.Object, "spec", "ipv4", "cidrs")
	cidrMap := cidrs[0].(map[string]interface{})
	assert.Equal(t, "10.200.0.0/24", cidrMap["cidr"])
}

func TestReconcileCiliumPool_NoopWhenNotCiliumProvider(t *testing.T) {
	net := &impdevv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "test-net", Namespace: "default"},
		Spec: impdevv1alpha1.ImpNetworkSpec{
			Subnet: "10.100.0.0/24",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(net).Build()
	r := &ImpNetworkReconciler{Client: fakeClient, Scheme: testScheme()}
	assert.NoError(t, r.reconcileCiliumPool(context.Background(), net))
	// No pool should have been created.
}
