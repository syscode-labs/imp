package agent

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

const testNode = "test-node"

func newReconciler(driver VMDriver) *ImpVMReconciler {
	return &ImpVMReconciler{
		Client:   k8sClient,
		NodeName: testNode,
		Driver:   driver,
	}
}

var _ = Describe("ImpVM Agent: Scheduled → Running", func() {
	ctx := context.Background()

	It("sets status.phase=Running, status.ip, status.runtimePID after Scheduled", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc1-scheduled", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseScheduled
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler(NewStubDriver()).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc1-scheduled", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc1-scheduled", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseRunning))
		Expect(updated.Status.IP).NotTo(BeEmpty())
		Expect(updated.Status.RuntimePID).To(BeNumerically(">", 0))
	})
})

var _ = Describe("ImpVM Agent: ephemeral exit → Succeeded", func() {
	ctx := context.Background()

	It("sets phase=Succeeded and clears spec.nodeName when ephemeral VM process exits", func() {
		driver := NewStubDriver()

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc2-ephemeral", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{
				NodeName:  testNode,
				Lifecycle: impdevv1alpha1.VMLifecycleEphemeral,
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		// Prime driver + status as if VM was already started.
		pid, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())
		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = pid
		vm.Status.IP = "192.168.100.1"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		// Simulate process exit.
		Expect(driver.Stop(ctx, vm)).To(Succeed())

		_, err = newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc2-ephemeral", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc2-ephemeral", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseSucceeded))
		Expect(updated.Spec.NodeName).To(BeEmpty())
	})
})

var _ = Describe("ImpVM Agent: persistent exit → Failed", func() {
	ctx := context.Background()

	It("sets phase=Failed and keeps spec.nodeName when persistent VM process exits", func() {
		driver := NewStubDriver()

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc3-persistent", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{
				NodeName:  testNode,
				Lifecycle: impdevv1alpha1.VMLifecyclePersistent,
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		pid, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())
		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = pid
		vm.Status.IP = "192.168.100.2"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		Expect(driver.Stop(ctx, vm)).To(Succeed())

		_, err = newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc3-persistent", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc3-persistent", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseFailed))
		Expect(updated.Spec.NodeName).To(Equal(testNode)) // persistent: keep nodeName
	})
})

var _ = Describe("ImpVM Agent: Terminating → clears nodeName", func() {
	ctx := context.Background()

	It("calls Stop, clears spec.nodeName, and clears status.ip + status.runtimePID", func() {
		driver := NewStubDriver()

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc4-terminating", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		pid, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())
		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseTerminating
		vm.Status.RuntimePID = pid
		vm.Status.IP = "192.168.100.3"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err = newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc4-terminating", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc4-terminating", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.IP).To(BeEmpty())
		Expect(updated.Status.RuntimePID).To(BeZero())
	})
})
