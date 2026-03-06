package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newSnapshotReconciler() *ImpVMSnapshotReconciler {
	return &ImpVMSnapshotReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
}

// makeParentSnap builds a minimal parent ImpVMSnapshot (no LabelSnapshotParent label).
func makeParentSnap(name, ns string, extraSpec func(*impv1alpha1.ImpVMSnapshotSpec)) *impv1alpha1.ImpVMSnapshot {
	snap := &impv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: impv1alpha1.ImpVMSnapshotSpec{
			SourceVMName:      "vm-does-not-exist",
			SourceVMNamespace: ns,
			Storage:           impv1alpha1.SnapshotStorageSpec{Type: "node-local"},
			Retention:         3,
		},
	}
	if extraSpec != nil {
		extraSpec(&snap.Spec)
	}
	return snap
}

// makeChildSnap creates a child ImpVMSnapshot with LabelSnapshotParent set.
// Uses plain string APIVersion/Kind since envtest doesn't populate TypeMeta on fetched objects.
func makeChildSnap(name, ns, parentName string, parentUID types.UID) *impv1alpha1.ImpVMSnapshot {
	t := true
	return &impv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{impv1alpha1.LabelSnapshotParent: parentName},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "imp.dev/v1alpha1",
					Kind:               "ImpVMSnapshot",
					Name:               parentName,
					UID:                parentUID,
					Controller:         &t,
					BlockOwnerDeletion: &t,
				},
			},
		},
		Spec: impv1alpha1.ImpVMSnapshotSpec{
			SourceVMName:      "vm-does-not-exist",
			SourceVMNamespace: ns,
			Storage:           impv1alpha1.SnapshotStorageSpec{Type: "node-local"},
		},
	}
}

var _ = Describe("ImpVMSnapshot controller", func() {
	ctx := context.Background()

	It("sets phase to Pending on creation", func() {
		snap := &impv1alpha1.ImpVMSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "snap-ctrl-test",
				Namespace: "default",
			},
			Spec: impv1alpha1.ImpVMSnapshotSpec{
				SourceVMName:      "vm-does-not-exist",
				SourceVMNamespace: "default",
				Storage:           impv1alpha1.SnapshotStorageSpec{Type: "node-local"},
			},
		}
		Expect(k8sClient.Create(ctx, snap)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, snap) }) //nolint:errcheck

		r := newSnapshotReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "snap-ctrl-test", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impv1alpha1.ImpVMSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "snap-ctrl-test", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).NotTo(BeEmpty())
		Expect(updated.Status.Phase).To(Equal("Pending"))
	})

	It("TestOperatorSnapshotReconciler_createsChild — creates one child for one-shot parent", func() {
		parent := makeParentSnap("snap-parent-creates", "default", nil)
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(func() {
			list := &impv1alpha1.ImpVMSnapshotList{}
			_ = k8sClient.List(ctx, list, client.InNamespace("default"), client.MatchingLabels{impv1alpha1.LabelSnapshotParent: "snap-parent-creates"})
			for i := range list.Items {
				_ = k8sClient.Delete(ctx, &list.Items[i])
			}
			_ = k8sClient.Delete(ctx, parent)
		})

		r := newSnapshotReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "snap-parent-creates", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		children := &impv1alpha1.ImpVMSnapshotList{}
		Expect(k8sClient.List(ctx, children, client.InNamespace("default"),
			client.MatchingLabels{impv1alpha1.LabelSnapshotParent: "snap-parent-creates"},
		)).To(Succeed())
		Expect(children.Items).To(HaveLen(1))
	})

	It("TestOperatorSnapshotReconciler_prunesOldChildren — prunes beyond retention=2", func() {
		parentName := "snap-parent-prune"
		parent := makeParentSnap(parentName, "default", func(s *impv1alpha1.ImpVMSnapshotSpec) {
			s.Retention = 2
		})
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(func() {
			list := &impv1alpha1.ImpVMSnapshotList{}
			_ = k8sClient.List(ctx, list, client.InNamespace("default"), client.MatchingLabels{impv1alpha1.LabelSnapshotParent: parentName})
			for i := range list.Items {
				_ = k8sClient.Delete(ctx, &list.Items[i])
			}
			_ = k8sClient.Delete(ctx, parent)
		})

		fetched := &impv1alpha1.ImpVMSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: parentName, Namespace: "default"}, fetched)).To(Succeed())

		// Create 3 terminated children
		now := metav1.Now()
		for i := 0; i < 3; i++ {
			child := makeChildSnap(fmt.Sprintf("%s-exec-%d", parentName, i), "default", parentName, fetched.UID)
			Expect(k8sClient.Create(ctx, child)).To(Succeed())
			child.Status.Phase = "Succeeded"
			child.Status.TerminatedAt = &now
			Expect(k8sClient.Status().Update(ctx, child)).To(Succeed())
		}

		r := newSnapshotReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: parentName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		children := &impv1alpha1.ImpVMSnapshotList{}
		Expect(k8sClient.List(ctx, children, client.InNamespace("default"),
			client.MatchingLabels{impv1alpha1.LabelSnapshotParent: parentName},
		)).To(Succeed())
		// retention=2; oldest (exec-0) should be pruned; up to retention+1 may exist if a new one was created
		names := make([]string, len(children.Items))
		for i, c := range children.Items {
			names[i] = c.Name
		}
		Expect(names).NotTo(ContainElement(fmt.Sprintf("%s-exec-0", parentName)))
		Expect(len(children.Items)).To(BeNumerically("<=", 2))
	})

	It("TestOperatorSnapshotReconciler_skipsIfActiveChild — does not create when active child exists", func() {
		parentName := "snap-parent-active"
		parent := makeParentSnap(parentName, "default", nil)
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(func() {
			list := &impv1alpha1.ImpVMSnapshotList{}
			_ = k8sClient.List(ctx, list, client.InNamespace("default"), client.MatchingLabels{impv1alpha1.LabelSnapshotParent: parentName})
			for i := range list.Items {
				_ = k8sClient.Delete(ctx, &list.Items[i])
			}
			_ = k8sClient.Delete(ctx, parent)
		})

		fetched := &impv1alpha1.ImpVMSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: parentName, Namespace: "default"}, fetched)).To(Succeed())

		// Create 1 active child (TerminatedAt == nil)
		activeChild := makeChildSnap(parentName+"-active", "default", parentName, fetched.UID)
		Expect(k8sClient.Create(ctx, activeChild)).To(Succeed())
		// TerminatedAt deliberately NOT set — child is active

		r := newSnapshotReconciler()
		result, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: parentName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		children := &impv1alpha1.ImpVMSnapshotList{}
		Expect(k8sClient.List(ctx, children, client.InNamespace("default"),
			client.MatchingLabels{impv1alpha1.LabelSnapshotParent: parentName},
		)).To(Succeed())
		Expect(children.Items).To(HaveLen(1)) // no new child created
	})

	It("TestOperatorSnapshotReconciler_validatesBaseSnapshot — sets status.baseSnapshot when child Succeeded", func() {
		parentName := "snap-parent-base"
		childName := "snap-parent-exec-1"
		parent := makeParentSnap(parentName, "default", func(s *impv1alpha1.ImpVMSnapshotSpec) {
			s.BaseSnapshot = childName
		})
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(func() {
			list := &impv1alpha1.ImpVMSnapshotList{}
			_ = k8sClient.List(ctx, list, client.InNamespace("default"), client.MatchingLabels{impv1alpha1.LabelSnapshotParent: parentName})
			for i := range list.Items {
				_ = k8sClient.Delete(ctx, &list.Items[i])
			}
			_ = k8sClient.Delete(ctx, parent)
		})

		fetched := &impv1alpha1.ImpVMSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: parentName, Namespace: "default"}, fetched)).To(Succeed())

		now := metav1.Now()
		child := makeChildSnap(childName, "default", parentName, fetched.UID)
		Expect(k8sClient.Create(ctx, child)).To(Succeed())
		child.Status.Phase = "Succeeded"
		child.Status.TerminatedAt = &now
		Expect(k8sClient.Status().Update(ctx, child)).To(Succeed())

		r := newSnapshotReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: parentName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impv1alpha1.ImpVMSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: parentName, Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.BaseSnapshot).To(Equal(childName))
	})

	It("TestOperatorSnapshotReconciler_baseSnapshotExemptFromPruning — baseSnapshot child is never pruned", func() {
		parentName := "snap-parent-exempt"
		baseChildName := "snap-parent-exec-0"
		otherChildName := "snap-parent-exec-1"
		parent := makeParentSnap(parentName, "default", func(s *impv1alpha1.ImpVMSnapshotSpec) {
			s.Retention = 1
			s.BaseSnapshot = baseChildName
		})
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(func() {
			list := &impv1alpha1.ImpVMSnapshotList{}
			_ = k8sClient.List(ctx, list, client.InNamespace("default"), client.MatchingLabels{impv1alpha1.LabelSnapshotParent: parentName})
			for i := range list.Items {
				_ = k8sClient.Delete(ctx, &list.Items[i])
			}
			_ = k8sClient.Delete(ctx, parent)
		})

		fetched := &impv1alpha1.ImpVMSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: parentName, Namespace: "default"}, fetched)).To(Succeed())

		now := metav1.Now()
		// Create baseSnapshot child (oldest, snap-parent-exec-0).
		baseChild := makeChildSnap(baseChildName, "default", parentName, fetched.UID)
		Expect(k8sClient.Create(ctx, baseChild)).To(Succeed())
		baseChild.Status.Phase = "Succeeded"
		baseChild.Status.TerminatedAt = &now
		Expect(k8sClient.Status().Update(ctx, baseChild)).To(Succeed())

		// Create a second terminated child (newer, snap-parent-exec-1).
		otherChild := makeChildSnap(otherChildName, "default", parentName, fetched.UID)
		Expect(k8sClient.Create(ctx, otherChild)).To(Succeed())
		otherChild.Status.Phase = "Succeeded"
		otherChild.Status.TerminatedAt = &now
		Expect(k8sClient.Status().Update(ctx, otherChild)).To(Succeed())

		r := newSnapshotReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: parentName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		// The elected baseSnapshot child must survive pruning even though it is the oldest.
		surviving := &impv1alpha1.ImpVMSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: baseChildName, Namespace: "default"}, surviving)).To(Succeed())

		// status.baseSnapshot should also be elected.
		updatedParent := &impv1alpha1.ImpVMSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: parentName, Namespace: "default"}, updatedParent)).To(Succeed())
		Expect(updatedParent.Status.BaseSnapshot).To(Equal(baseChildName))
	})
})
