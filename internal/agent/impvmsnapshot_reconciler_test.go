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
)

func newSnapshotTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = impdevv1alpha1.AddToScheme(s)
	return s
}

func TestSnapshotReconciler_skipsWrongNode(t *testing.T) {
	vm := &impdevv1alpha1.ImpVM{}
	vm.Name, vm.Namespace = "my-vm", "default"
	vm.Spec.NodeName = "other-node"

	snap := &impdevv1alpha1.ImpVMSnapshot{}
	snap.Name, snap.Namespace = "snap-child", "default"
	snap.Labels = map[string]string{impdevv1alpha1.LabelSnapshotParent: "snap-parent"}
	snap.Spec.SourceVMName = "my-vm"
	snap.Spec.SourceVMNamespace = "default"
	snap.Spec.Storage = impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"}

	scheme := newSnapshotTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(vm, snap).
		WithStatusSubresource(snap).
		Build()

	r := &agent.ImpVMSnapshotReconciler{
		Client:   c,
		Scheme:   scheme,
		NodeName: "this-node",
		Driver:   agent.NewStubDriver(),
	}

	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "snap-child", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("expected no-op result for wrong node, got %+v", res)
	}
	// Verify TerminatedAt was NOT set (we did nothing)
	fetched := &impdevv1alpha1.ImpVMSnapshot{}
	_ = c.Get(context.Background(), types.NamespacedName{Name: "snap-child", Namespace: "default"}, fetched)
	if fetched.Status.TerminatedAt != nil {
		t.Error("expected TerminatedAt to be nil for wrong-node skip")
	}
}

func TestSnapshotReconciler_skipsParentObjects(t *testing.T) {
	// A snap with NO LabelSnapshotParent label is a parent object — skip it.
	snap := &impdevv1alpha1.ImpVMSnapshot{}
	snap.Name, snap.Namespace = "snap-parent", "default"
	snap.Spec.SourceVMName = "my-vm"
	snap.Spec.SourceVMNamespace = "default"
	snap.Spec.Storage = impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"}

	scheme := newSnapshotTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(snap).
		WithStatusSubresource(snap).
		Build()

	r := &agent.ImpVMSnapshotReconciler{
		Client:   c,
		Scheme:   scheme,
		NodeName: "this-node",
		Driver:   agent.NewStubDriver(),
	}

	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "snap-parent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("expected no-op for parent object, got %+v", res)
	}
}

func TestSnapshotReconciler_skipsAlreadyTerminated(t *testing.T) {
	vm := &impdevv1alpha1.ImpVM{}
	vm.Name, vm.Namespace = "my-vm", "default"
	vm.Spec.NodeName = "this-node"

	now := metav1.Now()
	snap := &impdevv1alpha1.ImpVMSnapshot{}
	snap.Name, snap.Namespace = "snap-child", "default"
	snap.Labels = map[string]string{impdevv1alpha1.LabelSnapshotParent: "snap-parent"}
	snap.Spec.SourceVMName = "my-vm"
	snap.Spec.SourceVMNamespace = "default"
	snap.Spec.Storage = impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"}
	snap.Status.TerminatedAt = &now
	snap.Status.Phase = "Succeeded"

	scheme := newSnapshotTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(vm, snap).
		WithStatusSubresource(snap).
		Build()

	r := &agent.ImpVMSnapshotReconciler{
		Client:   c,
		Scheme:   scheme,
		NodeName: "this-node",
		Driver:   agent.NewStubDriver(),
	}

	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "snap-child", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = res // no-op
}
