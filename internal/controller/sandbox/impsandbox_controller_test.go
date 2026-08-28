/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sandbox

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	sandboxv1alpha1 "github.com/syscode-labs/imp/api/sandbox/v1alpha1"
	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
	"github.com/syscode-labs/imp/internal/sandboxgateway"
)

func newReconcilerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, impv1alpha1.AddToScheme(s))
	require.NoError(t, sandboxv1alpha1.AddToScheme(s))
	return s
}

func newTestSandbox() *sandboxv1alpha1.ImpSandbox {
	return &sandboxv1alpha1.ImpSandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "sb", Namespace: "team-a"},
		Spec: sandboxv1alpha1.ImpSandboxSpec{
			ClassRef: &impv1alpha1.ClusterObjectRef{Name: "small"},
			Image:    "alpine:3",
			Tenancy:  sandboxv1alpha1.TenancyStandard,
		},
	}
}

type reconcilerFixture struct {
	r        *ImpSandboxReconciler
	scheme   *runtime.Scheme
	cniStore *cnidetect.Store
}

func newFixture(t *testing.T, objs ...client.Object) *reconcilerFixture {
	t.Helper()
	scheme := newReconcilerScheme(t)
	cniStore := &cnidetect.Store{}
	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&sandboxv1alpha1.ImpSandbox{}).
		WithObjects(objs...)
	return &reconcilerFixture{
		scheme:   scheme,
		cniStore: cniStore,
		r: &ImpSandboxReconciler{
			Client:   builder.Build(),
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(64),
			CNIStore: cniStore,
		},
	}
}

func TestReconcile_createsNetworkAndVM(t *testing.T) {
	f := newFixture(t, newTestSandbox())

	res, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)
	assert.False(t, res.RequeueAfter > 0)

	net := &impv1alpha1.ImpNetwork{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb-net"}, net))
	assert.True(t, net.Spec.NAT.Enabled)
	assert.Equal(t, "10.214", net.Spec.Subnet[:len("10.214")])

	vm := &impv1alpha1.ImpVM{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb"}, vm))
	assert.Equal(t, impv1alpha1.VMLifecyclePersistent, vm.Spec.Lifecycle)
	assert.Equal(t, "sb-net", vm.Spec.NetworkRef.Name)
	assert.Equal(t, "sb", vm.Labels[sandboxv1alpha1.SandboxOwnerLabel])

	sb := &sandboxv1alpha1.ImpSandbox{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb"}, sb))
	assert.Equal(t, "sb", sb.Status.VMName)
	assert.Equal(t, sandboxv1alpha1.TenancyStandard, sb.Status.EffectiveTenancy)
}

func TestReconcile_recreatesDeletedVM(t *testing.T) {
	f := newFixture(t, newTestSandbox())
	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}}

	_, err := f.r.Reconcile(ctx, req)
	require.NoError(t, err)

	vm := &impv1alpha1.ImpVM{}
	require.NoError(t, f.r.Get(ctx, types.NamespacedName{Namespace: "team-a", Name: "sb"}, vm))
	require.NoError(t, f.r.Delete(ctx, vm))

	_, err = f.r.Reconcile(ctx, req)
	require.NoError(t, err)

	err = f.r.Get(ctx, types.NamespacedName{Namespace: "team-a", Name: "sb"}, vm)
	require.NoError(t, err)
}

func TestReconcile_hardWithoutCiliumIsDenied(t *testing.T) {
	sb := newTestSandbox()
	sb.Spec.Tenancy = sandboxv1alpha1.TenancyHard
	f := newFixture(t, sb)

	res, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)
	assert.True(t, res.RequeueAfter == retryInterval)

	vmList := &impv1alpha1.ImpVMList{}
	require.NoError(t, f.r.List(context.Background(), vmList))
	assert.Empty(t, vmList.Items, "no VM must be created when hard tenancy cannot be enforced")

	got := &sandboxv1alpha1.ImpSandbox{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb"}, got))
	cond := findCondition(got.Status.Conditions, ConditionTenancyEnforced)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Contains(t, cond.Message, "requires Cilium")
}

func TestReconcile_hardWithCiliumProceeds(t *testing.T) {
	sb := newTestSandbox()
	sb.Spec.Tenancy = sandboxv1alpha1.TenancyHard
	f := newFixture(t, sb)
	f.cniStore.Set(cnidetect.Result{Provider: cnidetect.ProviderCilium})

	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)

	vmList := &impv1alpha1.ImpVMList{}
	require.NoError(t, f.r.List(context.Background(), vmList))
	require.Len(t, vmList.Items, 1)

	got := &sandboxv1alpha1.ImpSandbox{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb"}, got))
	assert.Equal(t, sandboxv1alpha1.TenancyHard, got.Status.EffectiveTenancy)
}

func TestReconcile_floorBelowHardDeniedAtReconcilerToo(t *testing.T) {
	cfg := &impv1alpha1.ClusterImpConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: impv1alpha1.ClusterImpConfigSpec{
			Sandbox: &impv1alpha1.SandboxConfig{FloorTenancy: "hard"},
		},
	}
	f := newFixture(t, newTestSandbox(), cfg)

	res, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)
	assert.True(t, res.RequeueAfter == retryInterval)

	vmList := &impv1alpha1.ImpVMList{}
	require.NoError(t, f.r.List(context.Background(), vmList))
	assert.Empty(t, vmList.Items)
}

func TestReconcile_attachesReferencedNetworkWithoutCreatingOne(t *testing.T) {
	sb := newTestSandbox()
	sb.Spec.NetworkRef = &impv1alpha1.LocalObjectRef{Name: "shared-net"}
	f := newFixture(t, sb)

	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)

	networks := &impv1alpha1.ImpNetworkList{}
	require.NoError(t, f.r.List(context.Background(), networks))
	assert.Empty(t, networks.Items, "referenced networks are not duplicated")
}

func TestAllocateSubnet_lowestFreeSequential(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.r.allocateSubnet(ctx)
	require.NoError(t, err)
	assert.Equal(t, "10.214.0.0/30", first.cidr)
	assert.Equal(t, "0", first.index)

	// Simulate the first allocation being taken.
	f.r.Create(ctx, &impv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "n0"},
		Spec:       impv1alpha1.ImpNetworkSpec{Subnet: "10.214.0.0/30"},
	})

	second, err := f.r.allocateSubnet(ctx)
	require.NoError(t, err)
	assert.Equal(t, "10.214.0.4/30", second.cidr)
	assert.Equal(t, "1", second.index)
}

func TestAllocateSubnet_skipsForeignOverlappingNetworks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A legacy /24 inside the base block occupies /30 slots 0..63 entirely.
	require.NoError(t, f.r.Create(ctx, &impv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "legacy"},
		Spec:       impv1alpha1.ImpNetworkSpec{Subnet: "10.214.0.0/24"},
	}))

	got, err := f.r.allocateSubnet(ctx)
	require.NoError(t, err)
	assert.Equal(t, "10.214.1.0/30", got.cidr, "must start after the foreign /24")
	assert.Equal(t, "64", got.index)

	idx, convErr := strconv.Atoi(got.index)
	require.NoError(t, convErr)
	assert.GreaterOrEqual(t, idx, 64)
}

func TestAllocateSubnet_ignoresOutOfRangeNetworks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	require.NoError(t, f.r.Create(ctx, &impv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "user-net"},
		Spec:       impv1alpha1.ImpNetworkSpec{Subnet: "192.168.100.0/24"},
	}))
	require.NoError(t, f.r.Create(ctx, &impv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "bad-net"},
		Spec:       impv1alpha1.ImpNetworkSpec{Subnet: "not-a-cidr"},
	}))

	got, err := f.r.allocateSubnet(ctx)
	require.NoError(t, err)
	assert.Equal(t, "10.214.0.0/30", got.cidr)
}

func TestBaselineDenyCIDRs_discoversTopology(t *testing.T) {
	node1 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Spec: corev1.NodeSpec{PodCIDRs: []string{"10.244.0.0/24"}}}
	node2 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n2"}, Spec: corev1.NodeSpec{PodCIDRs: []string{"10.244.1.0/24"}}}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "kubernetes"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.1"},
	}
	f := newFixture(t, node1, node2, svc)

	deny := f.r.baselineDenyCIDRs(context.Background())
	assert.Contains(t, deny, "169.254.169.254/32")
	assert.Contains(t, deny, "10.244.0.0/24")
	assert.Contains(t, deny, "10.244.1.0/24")
	assert.Contains(t, deny, "10.96.0.1/32")
	assert.Len(t, deny, 4, "no duplicates expected")
}

func TestEnsureNetwork_setsFirewallDenyList(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Spec: corev1.NodeSpec{PodCIDR: "10.244.0.0/24", PodCIDRs: []string{"10.244.0.0/24"}}}
	f := newFixture(t, newTestSandbox(), node)

	res, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)
	assert.Zero(t, res.RequeueAfter)

	net := &impv1alpha1.ImpNetwork{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb-net"}, net))
	require.NotNil(t, net.Spec.Firewall)
	assert.Contains(t, net.Spec.Firewall.DenyCIDRs, "169.254.169.254/32")
	assert.Contains(t, net.Spec.Firewall.DenyCIDRs, "10.244.0.0/24")
}

func TestDelete_removesBackingVMBeforeFinalizerRelease(t *testing.T) {
	ctx := context.Background()

	// Deleting sandbox with its finalizer and backing VM already in place.
	// The fake client rejects setting deletionTimestamp post-create, so the
	// deleted state is constructed directly.
	now := metav1.Now()
	sb := newTestSandbox()
	sb.Finalizers = []string{sandboxFinalizer}
	sb.DeletionTimestamp = &now
	vm := &impv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "sb"},
	}
	f := newFixture(t, sb, vm)

	res, err := f.r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)
	assert.True(t, res.RequeueAfter == retryInterval, "waits while backing VM still exists")

	// The controller already issued the VM delete; once it is gone the
	// finalizer is released. Tolerate NotFound since deletion is async.
	require.NoError(t, client.IgnoreNotFound(f.r.Delete(ctx, vm)))
	_, err = f.r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)

	// With the finalizer released the fake client garbage-collects the
	// sandbox — assert that terminal state.
	final := &sandboxv1alpha1.ImpSandbox{}
	err = f.r.Get(ctx, types.NamespacedName{Namespace: "team-a", Name: "sb"}, final)
	assert.True(t, apierrors.IsNotFound(err), "sandbox must be gone after teardown, got %v", err)
}

func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

func TestSessionSecret_mintedAndDelivered(t *testing.T) {
	sb := newTestSandbox()
	sb.UID = "uid-1234"
	f := newFixture(t, sb)
	f.r.SessionHMACKey = []byte("test-cluster-key-32-bytes-long!!")

	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)

	sec := &corev1.Secret{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb-session"}, sec))
	want := sandboxgateway.Token([]byte("test-cluster-key-32-bytes-long!!"), "uid-1234")
	assert.Equal(t, want, string(sec.Data["token"]))

	got := &sandboxv1alpha1.ImpSandbox{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb"}, got))
	require.NotNil(t, got.Status.SessionSecretRef)
	assert.Equal(t, "sb-session", got.Status.SessionSecretRef.Name)
}

func TestSessionSecret_skippedWithoutKey(t *testing.T) {
	f := newFixture(t, newTestSandbox())

	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)

	secrets := &corev1.SecretList{}
	require.NoError(t, f.r.List(context.Background(), secrets))
	assert.Empty(t, secrets.Items)

	got := &sandboxv1alpha1.ImpSandbox{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb"}, got))
	assert.Nil(t, got.Status.SessionSecretRef)
}

func TestSessionSecret_rotatesOnKeyChange(t *testing.T) {
	sb := newTestSandbox()
	sb.UID = "uid-rotate"
	f := newFixture(t, sb)
	f.r.SessionHMACKey = []byte("old-key-old-key-old-key-old-key!!")

	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)

	// Cluster key rotates.
	f.r.SessionHMACKey = []byte("new-key-new-key-new-key-new-key!!")
	_, err = f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "sb"}})
	require.NoError(t, err)

	sec := &corev1.Secret{}
	require.NoError(t, f.r.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "sb-session"}, sec))
	assert.Equal(t, sandboxgateway.Token([]byte("new-key-new-key-new-key-new-key!!"), "uid-rotate"), string(sec.Data["token"]))
}
