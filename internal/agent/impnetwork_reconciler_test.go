//go:build linux

package agent_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
	"github.com/syscode-labs/imp/internal/agent/network"
)

func newImpNetworkScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = impdevv1alpha1.AddToScheme(s)
	return s
}

func TestImpNetworkReconciler_noLocalVMs(t *testing.T) {
	impNet := &impdevv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "net1",
			Namespace: "default",
			UID:       types.UID("uid-net1"),
		},
		Spec: impdevv1alpha1.ImpNetworkSpec{Subnet: "10.0.0.0/24"},
	}
	impNet.Status.VTEPTable = []impdevv1alpha1.VTEPEntry{
		{NodeIP: "192.168.1.2", VMIP: "10.0.0.5", VMMAC: "02:aa:bb:cc:dd:ee"},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newImpNetworkScheme()).
		WithObjects(impNet).
		WithStatusSubresource(impNet).
		Build()

	stub := &network.StubNetManager{}
	r := &agent.ImpNetworkReconciler{
		Client:   fakeClient,
		NodeName: "node-a",
		NodeIP:   "192.168.1.1",
		Net:      stub,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "net1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.EnsureVXLANCalls) != 0 {
		t.Errorf("expected no EnsureVXLAN calls (no local VMs), got %d", len(stub.EnsureVXLANCalls))
	}
}

func TestImpNetworkReconciler_withLocalVM(t *testing.T) {
	impNet := &impdevv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "net1",
			Namespace: "default",
			UID:       types.UID("uid-net1"),
		},
		Spec: impdevv1alpha1.ImpNetworkSpec{Subnet: "10.0.0.0/24"},
	}
	impNet.Status.VTEPTable = []impdevv1alpha1.VTEPEntry{
		{NodeIP: "192.168.1.1", VMIP: "10.0.0.2", VMMAC: "02:aa:00:00:00:01"},
		{NodeIP: "192.168.1.2", VMIP: "10.0.0.5", VMMAC: "02:bb:00:00:00:02"},
	}

	localVM := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-local", Namespace: "default"},
		Spec: impdevv1alpha1.ImpVMSpec{
			NodeName:   "node-a",
			NetworkRef: &impdevv1alpha1.NetworkRef{Name: "net1"},
		},
	}
	localVM.Status.Phase = impdevv1alpha1.VMPhaseRunning

	fakeClient := fake.NewClientBuilder().
		WithScheme(newImpNetworkScheme()).
		WithObjects(impNet, localVM).
		WithStatusSubresource(impNet, localVM).
		Build()

	stub := &network.StubNetManager{}
	r := &agent.ImpNetworkReconciler{
		Client:   fakeClient,
		NodeName: "node-a",
		NodeIP:   "192.168.1.1",
		Net:      stub,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "net1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.EnsureVXLANCalls) != 1 {
		t.Fatalf("expected 1 EnsureVXLAN call, got %d", len(stub.EnsureVXLANCalls))
	}
	if len(stub.SyncFDBCalls) != 1 {
		t.Fatalf("expected 1 SyncFDB call, got %d", len(stub.SyncFDBCalls))
	}
}
