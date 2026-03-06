package controller

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newMigrationTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, impv1alpha1.AddToScheme(s))
	return s
}

// TestMigrationReconciler_snapshotting_waitForSnapshot verifies that the reconciler
// requeues when the child snapshot has not yet reached a terminal state.
func TestMigrationReconciler_snapshotting_waitForSnapshot(t *testing.T) {
	scheme := newMigrationTestScheme(t)

	now := metav1.Now()
	mig := &impv1alpha1.ImpVMMigration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mig-1",
			Namespace: "default",
		},
		Spec: impv1alpha1.ImpVMMigrationSpec{
			SourceVMName:      "src-vm",
			SourceVMNamespace: "default",
			TargetNode:        "node-2",
		},
		Status: impv1alpha1.ImpVMMigrationStatus{
			Phase:        "Snapshotting",
			SnapshotRef:  "mig-1-snap",
			SelectedNode: "node-2",
		},
	}
	_ = now

	// Snapshot exists but TerminatedAt is nil — still in progress.
	snap := &impv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mig-1-snap",
			Namespace: "default",
		},
		Spec: impv1alpha1.ImpVMSnapshotSpec{
			SourceVMName:      "src-vm",
			SourceVMNamespace: "default",
			Storage:           impv1alpha1.SnapshotStorageSpec{Type: "node-local"},
		},
		Status: impv1alpha1.ImpVMSnapshotStatus{
			Phase:        "Running",
			TerminatedAt: nil,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&impv1alpha1.ImpVMMigration{}).
		WithObjects(mig, snap).
		Build()

	// Manually set the status (fake client requires WithStatusSubresource for Status().Patch).
	migWithStatus := mig.DeepCopy()
	migWithStatus.Status = mig.Status
	require.NoError(t, fakeClient.Status().Update(context.Background(), migWithStatus))

	r := &ImpVMMigrationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: "mig-1"},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0), "should requeue while snapshot is in progress")
}

// TestMigrationReconciler_snapshotFailed_setsFailedPhase verifies that a failed snapshot
// causes the migration to transition to Phase="Failed".
func TestMigrationReconciler_snapshotFailed_setsFailedPhase(t *testing.T) {
	scheme := newMigrationTestScheme(t)

	mig := &impv1alpha1.ImpVMMigration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mig-2",
			Namespace: "default",
		},
		Spec: impv1alpha1.ImpVMMigrationSpec{
			SourceVMName:      "src-vm",
			SourceVMNamespace: "default",
			TargetNode:        "node-2",
		},
		Status: impv1alpha1.ImpVMMigrationStatus{
			Phase:        "Snapshotting",
			SnapshotRef:  "mig-2-snap",
			SelectedNode: "node-2",
		},
	}

	terminated := metav1.Now()
	snap := &impv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mig-2-snap",
			Namespace: "default",
		},
		Spec: impv1alpha1.ImpVMSnapshotSpec{
			SourceVMName:      "src-vm",
			SourceVMNamespace: "default",
			Storage:           impv1alpha1.SnapshotStorageSpec{Type: "node-local"},
		},
		Status: impv1alpha1.ImpVMSnapshotStatus{
			Phase:        "Failed",
			TerminatedAt: &terminated,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&impv1alpha1.ImpVMMigration{}, &impv1alpha1.ImpVMSnapshot{}).
		WithObjects(mig, snap).
		Build()

	// Push status into the fake store.
	migWithStatus := mig.DeepCopy()
	require.NoError(t, fakeClient.Status().Update(context.Background(), migWithStatus))
	snapWithStatus := snap.DeepCopy()
	require.NoError(t, fakeClient.Status().Update(context.Background(), snapWithStatus))

	r := &ImpVMMigrationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: "mig-2"},
	})
	require.NoError(t, err)

	updated := &impv1alpha1.ImpVMMigration{}
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "mig-2"}, updated))
	assert.Equal(t, "Failed", updated.Status.Phase)
}

func TestMigrationReconciler_requeueWhenSourceVMUnscheduled(t *testing.T) {
	// Source VM has no NodeName yet → should requeue, not fail
	mig := &impv1alpha1.ImpVMMigration{}
	mig.Name, mig.Namespace = "mig-unsched", "default"
	mig.Spec.SourceVMName = "vm-unsched"
	mig.Spec.SourceVMNamespace = "default"
	mig.Status.Phase = "Pending"

	vm := &impv1alpha1.ImpVM{}
	vm.Name, vm.Namespace = "vm-unsched", "default"
	// NodeName is empty — VM not yet scheduled

	scheme := newMigrationTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mig, vm).WithStatusSubresource(mig).Build()
	// Push status into the fake store.
	migWithStatus := mig.DeepCopy()
	require.NoError(t, c.Status().Update(context.Background(), migWithStatus))

	r := &ImpVMMigrationReconciler{Client: c, Scheme: scheme}

	res, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "mig-unsched", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RequeueAfter == 0 && !res.Requeue {
		t.Error("expected requeue when source VM has no NodeName")
	}

	var updated impv1alpha1.ImpVMMigration
	_ = c.Get(context.Background(), types.NamespacedName{Name: "mig-unsched", Namespace: "default"}, &updated)
	if updated.Status.Phase == "Failed" {
		t.Error("migration should not be Failed when source VM is simply unscheduled")
	}
}

func newMigrationReconciler() *ImpVMMigrationReconciler {
	return &ImpVMMigrationReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
}

var _ = Describe("ImpVMMigration controller", func() {
	ctx := context.Background()

	It("sets phase to Failed when source VM is missing", func() {
		mig := &impv1alpha1.ImpVMMigration{
			ObjectMeta: metav1.ObjectMeta{Name: "mig-ctrl-test", Namespace: "default"},
			Spec: impv1alpha1.ImpVMMigrationSpec{
				SourceVMName:      "vm-that-does-not-exist",
				SourceVMNamespace: "default",
			},
		}
		Expect(k8sClient.Create(ctx, mig)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, mig) }) //nolint:errcheck

		r := newMigrationReconciler()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "mig-ctrl-test", Namespace: "default"},
		}
		// First reconcile: "" → "Pending" (requeues).
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		// Second reconcile: "Pending" → "Failed" (source VM missing).
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		updated := &impv1alpha1.ImpVMMigration{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mig-ctrl-test", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Failed"))
		Expect(updated.Status.Message).To(ContainSubstring("source VM not found"))
	})
})
