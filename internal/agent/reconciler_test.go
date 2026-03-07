//go:build linux

package agent

import (
	"context"
	"errors"
	"time"

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

var _ = Describe("ImpVM Agent: VMPhaseStarting → RequeueAfter 2s", func() {
	ctx := context.Background()

	It("returns RequeueAfter=2s and does not mutate status", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc5-starting", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseStarting
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		result, err := newReconciler(NewStubDriver()).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc5-starting", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(2 * time.Second))

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc5-starting", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseStarting))
	})
})

var _ = Describe("ImpVM Agent: nodeName filter", func() {
	ctx := context.Background()

	It("ignores VMs assigned to a different node", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc6-other-node", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: "other-node"},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseScheduled
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		result, err := newReconciler(NewStubDriver()).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc6-other-node", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		// Phase must remain Scheduled — reconciler did nothing.
		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc6-other-node", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseScheduled))
	})
})

var _ = Describe("ImpVM Agent: handleScheduled driver.Start error", func() {
	ctx := context.Background()

	It("returns error when driver.Start fails", func() {
		driver := NewStubDriver()
		driver.InjectStartError(errors.New("firecracker exploded"))

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc7-start-err", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseScheduled
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc7-start-err", Namespace: "default"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("firecracker exploded"))
	})
})

var _ = Describe("ImpVM Agent: handleRunning driver.Inspect error", func() {
	ctx := context.Background()

	It("returns error when driver.Inspect fails during Running phase", func() {
		driver := NewStubDriver()

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc8-inspect-err", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		// Inject inspect error before setting Running phase.
		driver.InjectInspectError(errors.New("vsock timeout"))

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc8-inspect-err", Namespace: "default"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("vsock timeout"))
	})
})

var _ = Describe("ImpVM Agent: handleScheduled Inspect error after Start succeeds", func() {
	ctx := context.Background()

	It("returns error when Inspect fails after a successful Start", func() {
		driver := NewStubDriver()
		// Start will succeed; Inspect will fail.
		driver.InjectInspectError(errors.New("inspect fail after start"))

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc10-sched-inspect-err", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseScheduled
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc10-sched-inspect-err", Namespace: "default"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("inspect fail after start"))
	})
})

var _ = Describe("ImpVM Agent: handleRunning VM still running (steady state)", func() {
	ctx := context.Background()

	It("returns no error and no mutation when VM process is still running", func() {
		driver := NewStubDriver()

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc11-still-running", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		// Prime driver so Inspect returns Running=true.
		_, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		result, err := newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc11-still-running", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc11-still-running", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseRunning))
	})
})

var _ = Describe("ImpVM Agent: Reconcile default phase (Pending)", func() {
	ctx := context.Background()

	It("does nothing for Pending phase (not this node's concern)", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc12-pending", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		// Phase is empty/Pending (default zero value) — falls through to default case.
		result, err := newReconciler(NewStubDriver()).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc12-pending", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
	})
})

var _ = Describe("ImpVM Agent: Reconcile non-existent VM (NotFound)", func() {
	ctx := context.Background()

	It("returns no error when VM not found (IgnoreNotFound path)", func() {
		result, err := newReconciler(NewStubDriver()).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "does-not-exist", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
	})
})

var _ = Describe("ImpVM Agent: Running — lazy reattach on agent restart", func() {
	ctx := context.Background()

	It("reattaches and stays Running when PID is alive", func() {
		driver := NewStubDriver()
		driver.IsAliveResult = true

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc-reattach-alive", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = 99999
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		// Inspect returns Running=false (no entry in states map — simulates restart)
		// IsAliveResult=true means the PID check passes

		_, err := newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc-reattach-alive", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc-reattach-alive", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseRunning))
		Expect(driver.ReattachCalls).To(HaveLen(1))
		Expect(driver.ReattachCalls[0]).To(Equal("default/tc-reattach-alive"))
	})

	It("transitions to Failed when PID is dead", func() {
		driver := NewStubDriver()
		driver.IsAliveResult = false

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc-reattach-dead", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = 99999
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc-reattach-dead", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc-reattach-dead", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseFailed))
		Expect(driver.ReattachCalls).To(BeEmpty())
	})

	It("transitions to Failed when RuntimePID is zero", func() {
		driver := NewStubDriver()
		driver.IsAliveResult = true // shouldn't matter — PID is 0

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc-reattach-nopid", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = 0
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc-reattach-nopid", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc-reattach-nopid", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseFailed))
		Expect(driver.ReattachCalls).To(BeEmpty())
	})
})

var _ = Describe("ImpVM Agent: handleTerminating driver.Stop error", func() {
	ctx := context.Background()

	It("returns error when driver.Stop fails", func() {
		driver := NewStubDriver()
		driver.InjectStopError(errors.New("cannot stop: process gone"))

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc9-stop-err", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseTerminating
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc9-stop-err", Namespace: "default"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot stop: process gone"))
	})
})
