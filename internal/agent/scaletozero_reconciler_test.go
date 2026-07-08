//go:build linux

package agent

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

var _ = Describe("ImpVM Agent: scale-to-zero", func() {
	ctx := context.Background()

	// szWith builds a ScaleToZero whose idle detector reads *bytes (so a test can
	// simulate traffic) and has no real packet source.
	szWith := func(bytes *uint64) *ScaleToZero {
		return newScaleToZero(func(string) (uint64, error) { return *bytes, nil }, nil, time.Second, 16)
	}

	It("auto-suspends a Running ScaleToZero VM after it goes idle", func() {
		driver := NewStubDriver()
		var bytes uint64 = 100
		sz := szWith(&bytes)
		r := &ImpVMReconciler{Client: k8sClient, NodeName: testNode, Driver: driver, SuspendDir: GinkgoT().TempDir(), SZ: sz}

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "tc-sz-idle", Namespace: "default", Finalizers: []string{"imp/finalizer"}},
			Spec:       impdevv1alpha1.ImpVMSpec{NodeName: testNode, DesiredState: impdevv1alpha1.VMDesiredStateScaleToZero},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		pid, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())
		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = pid
		vm.Status.IP = "192.168.100.60"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		key := types.NamespacedName{Name: "tc-sz-idle", Namespace: "default"}
		req := reconcile.Request{NamespacedName: key}

		// Seed the idle sample as if traffic last moved an hour ago, so the probe
		// (default 5m idleTimeout) sees the VM as idle immediately.
		sz.samples[key] = idleSample{bytes: bytes, since: time.Now().Add(-time.Hour)}

		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseSuspending))
	})

	It("keeps a Running ScaleToZero VM running while it has traffic", func() {
		driver := NewStubDriver()
		var bytes uint64 = 100
		sz := szWith(&bytes)
		r := &ImpVMReconciler{Client: k8sClient, NodeName: testNode, Driver: driver, SZ: sz}

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "tc-sz-busy", Namespace: "default", Finalizers: []string{"imp/finalizer"}},
			Spec:       impdevv1alpha1.ImpVMSpec{NodeName: testNode, DesiredState: impdevv1alpha1.VMDesiredStateScaleToZero},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		pid, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())
		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = pid
		vm.Status.IP = "192.168.100.61"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		key := types.NamespacedName{Name: "tc-sz-busy", Namespace: "default"}
		req := reconcile.Request{NamespacedName: key}

		// First reconcile establishes a baseline (never idle). Result requeues.
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(sz.interval))
		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseRunning))
	})

	It("resumes a Suspended ScaleToZero VM once a wake packet is observed", func() {
		driver := NewStubDriver()
		var bytes uint64 = 100
		sz := szWith(&bytes)
		suspendDir := GinkgoT().TempDir()
		r := &ImpVMReconciler{Client: k8sClient, NodeName: testNode, Driver: driver, SuspendDir: suspendDir, SZ: sz}

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "tc-sz-wake", Namespace: "default", Finalizers: []string{"imp/finalizer"}},
			Spec:       impdevv1alpha1.ImpVMSpec{NodeName: testNode, DesiredState: impdevv1alpha1.VMDesiredStateScaleToZero},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseSuspended
		vm.Status.IP = "192.168.100.62"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		key := types.NamespacedName{Name: "tc-sz-wake", Namespace: "default"}
		req := reconcile.Request{NamespacedName: key}

		// No wake yet → stays Suspended.
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseSuspended))

		// A packet arrives for the VM → registry marks it pending.
		sz.reg.register("192.168.100.62", vm)
		sz.reg.onDstIP("192.168.100.62")
		Expect(sz.reg.pending(key)).To(BeTrue())

		// Suspended + pending wake → Resuming.
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseResuming))

		// Resuming → Running; wake state cleared.
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseRunning))
		Expect(sz.reg.pending(key)).To(BeFalse())
	})
})
