package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newSnapshotReconciler() *ImpVMSnapshotReconciler {
	return &ImpVMSnapshotReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
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
})
