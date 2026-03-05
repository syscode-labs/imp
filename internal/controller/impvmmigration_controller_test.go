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
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "mig-ctrl-test", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impv1alpha1.ImpVMMigration{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mig-ctrl-test", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Failed"))
		Expect(updated.Status.Message).To(ContainSubstring("source VM not found"))
	})
})
