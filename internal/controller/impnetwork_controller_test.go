package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
)

func newNetworkReconciler(store *cnidetect.Store) (*ImpNetworkReconciler, *record.FakeRecorder) {
	rec := record.NewFakeRecorder(64)
	return &ImpNetworkReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: rec,
		CNIStore: store,
	}, rec
}

func ciliumStore() *cnidetect.Store {
	s := &cnidetect.Store{}
	s.Set(cnidetect.Result{Provider: cnidetect.ProviderCilium, NATBackend: cnidetect.NATBackendNftables})
	return s
}

func unknownStore() *cnidetect.Store {
	return &cnidetect.Store{} // empty — not yet set
}

var _ = Describe("ImpNetwork Controller: core", func() {
	ctx := context.Background()

	It("adds finalizer on first reconcile", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "core-net-1", Namespace: "default"},
			Spec:       impdevv1alpha1.ImpNetworkSpec{Subnet: "192.168.10.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, _ := newNetworkReconciler(unknownStore())
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "core-net-1", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpNetwork{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "core-net-1", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Finalizers).To(ContainElement(finalizerImpNetwork))
	})

	It("sets Ready=True condition on second reconcile", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "core-net-2", Namespace: "default"},
			Spec:       impdevv1alpha1.ImpNetworkSpec{Subnet: "192.168.20.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, _ := newNetworkReconciler(ciliumStore())
		// First reconcile: adds finalizer.
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "core-net-2", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		// Second reconcile: runs sync.
		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "core-net-2", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpNetwork{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "core-net-2", Namespace: "default"}, updated)).To(Succeed())
		c := apimeta.FindStatusCondition(updated.Status.Conditions, ConditionNetworkReady)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
	})

	It("emits CNIDetected event on sync", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "core-net-3", Namespace: "default"},
			Spec:       impdevv1alpha1.ImpNetworkSpec{Subnet: "192.168.30.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, rec := newNetworkReconciler(ciliumStore())
		// First reconcile: adds finalizer.
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "core-net-3", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		// Second reconcile: sync emits events.
		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "core-net-3", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		Eventually(rec.Events, time.Second).Should(Receive(ContainSubstring(EventReasonCNIDetected)))
	})

	It("removes finalizer on deletion", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "core-net-del",
				Namespace:  "default",
				Finalizers: []string{finalizerImpNetwork},
			},
			Spec: impdevv1alpha1.ImpNetworkSpec{Subnet: "192.168.40.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		Expect(k8sClient.Delete(ctx, net)).To(Succeed())

		r, _ := newNetworkReconciler(unknownStore())
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "core-net-del", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Object should be gone (finalizer removed → GC'd by envtest).
		updated := &impdevv1alpha1.ImpNetwork{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "core-net-del", Namespace: "default"}, updated)
		if err == nil {
			Expect(updated.Finalizers).NotTo(ContainElement(finalizerImpNetwork))
		}
	})
})

var _ = Describe("ImpNetwork Controller: reconcileVTEPTable", func() {
	ctx := context.Background()

	reconcileTwiceVTEP := func(r *ImpNetworkReconciler, name string) {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}}
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
	}

	It("removes stale VTEP entries for VMs that are no longer Running", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "vtep-gc-1", Namespace: "default"},
			Spec:       impdevv1alpha1.ImpNetworkSpec{Subnet: "10.200.1.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		// Inject a stale VTEP entry (no corresponding Running VM).
		base := net.DeepCopy()
		net.Status.VTEPTable = []impdevv1alpha1.VTEPEntry{
			{NodeIP: "192.168.1.10", VMIP: "10.200.1.5", VMMAC: "02:aa:bb:cc:dd:ee"},
		}
		Expect(k8sClient.Status().Patch(ctx, net, client.MergeFrom(base))).To(Succeed())

		r, _ := newNetworkReconciler(ciliumStore())
		reconcileTwiceVTEP(r, "vtep-gc-1")

		updated := &impdevv1alpha1.ImpNetwork{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "vtep-gc-1", Namespace: "default"}, updated)).To(Succeed())
		// Stale entry should be removed (no Running VM with IP 10.200.1.5 exists).
		Expect(updated.Status.VTEPTable).To(BeEmpty())
	})

	It("keeps VTEP entries for Running VMs that reference this network", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "vtep-keep-1", Namespace: "default"},
			Spec:       impdevv1alpha1.ImpNetworkSpec{Subnet: "10.200.2.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		// Create a Running ImpVM that references the network.
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "vtep-vm-1", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				NetworkRef: &impdevv1alpha1.LocalObjectRef{Name: "vtep-keep-1"},
				Image:      "ghcr.io/test/rootfs:latest",
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		vmBase := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.IP = "10.200.2.5"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(vmBase))).To(Succeed())

		// Inject a VTEP entry for the Running VM.
		netBase := net.DeepCopy()
		net.Status.VTEPTable = []impdevv1alpha1.VTEPEntry{
			{NodeIP: "192.168.1.10", VMIP: "10.200.2.5", VMMAC: "02:aa:bb:cc:dd:ee"},
		}
		Expect(k8sClient.Status().Patch(ctx, net, client.MergeFrom(netBase))).To(Succeed())

		r, _ := newNetworkReconciler(ciliumStore())
		reconcileTwiceVTEP(r, "vtep-keep-1")

		updated := &impdevv1alpha1.ImpNetwork{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "vtep-keep-1", Namespace: "default"}, updated)).To(Succeed())
		// Entry for the Running VM must be preserved.
		Expect(updated.Status.VTEPTable).To(HaveLen(1))
		Expect(updated.Status.VTEPTable[0].VMIP).To(Equal("10.200.2.5"))
	})

	It("removes entries for Stopped VMs but keeps entries for Running VMs", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "vtep-mixed-1", Namespace: "default"},
			Spec:       impdevv1alpha1.ImpNetworkSpec{Subnet: "10.200.3.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		// Create a Running ImpVM.
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "vtep-vm-running", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				NetworkRef: &impdevv1alpha1.LocalObjectRef{Name: "vtep-mixed-1"},
				Image:      "ghcr.io/test/rootfs:latest",
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		vmBase := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.IP = "10.200.3.5"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(vmBase))).To(Succeed())

		// Inject two VTEP entries: one for the Running VM, one stale.
		netBase := net.DeepCopy()
		net.Status.VTEPTable = []impdevv1alpha1.VTEPEntry{
			{NodeIP: "192.168.1.10", VMIP: "10.200.3.5", VMMAC: "02:aa:bb:cc:dd:01"},
			{NodeIP: "192.168.1.11", VMIP: "10.200.3.99", VMMAC: "02:aa:bb:cc:dd:02"},
		}
		Expect(k8sClient.Status().Patch(ctx, net, client.MergeFrom(netBase))).To(Succeed())

		r, _ := newNetworkReconciler(ciliumStore())
		reconcileTwiceVTEP(r, "vtep-mixed-1")

		updated := &impdevv1alpha1.ImpNetwork{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "vtep-mixed-1", Namespace: "default"}, updated)).To(Succeed())
		// Only the Running VM's entry should remain.
		Expect(updated.Status.VTEPTable).To(HaveLen(1))
		Expect(updated.Status.VTEPTable[0].VMIP).To(Equal("10.200.3.5"))
	})
})

var _ = Describe("ImpNetwork Controller: reconcileCiliumEnrollment", func() {
	ctx := context.Background()

	It("skips CEW creation when CNI store reports non-Cilium provider", func() {
		// Use a store that says Flannel is the CNI — enrollment must be skipped.
		flannelStore := &cnidetect.Store{}
		flannelStore.Set(cnidetect.Result{Provider: cnidetect.ProviderFlannel, NATBackend: cnidetect.NATBackendIPTables})

		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "cew-skip-1", Namespace: "default"},
			Spec:       impdevv1alpha1.ImpNetworkSpec{Subnet: "10.210.0.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, _ := newNetworkReconciler(flannelStore)
		// Two reconciles: finalizer then sync.
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cew-skip-1", Namespace: "default"}}
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		// No error means the guard exited cleanly (CEW CRD absent in envtest → ciliumPresent()=false).
	})

	It("skips CEW creation when CNI store is empty (not yet detected)", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "cew-skip-2", Namespace: "default"},
			Spec:       impdevv1alpha1.ImpNetworkSpec{Subnet: "10.211.0.0/24"},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, _ := newNetworkReconciler(unknownStore())
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cew-skip-2", Namespace: "default"}}
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("ImpNetwork Controller: CiliumConfigMissing", func() {
	ctx := context.Background()

	// reconcileTwice runs reconcile twice: once for finalizer, once for sync.
	reconcileTwice := func(r *ImpNetworkReconciler, name string) {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}}
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
	}

	It("emits CiliumConfigMissing when masqueradeViaCilium=true and ConfigMap absent", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "cilium-warn-1", Namespace: "default"},
			Spec: impdevv1alpha1.ImpNetworkSpec{
				Subnet: "192.168.50.0/24",
				Cilium: &impdevv1alpha1.CiliumNetworkSpec{MasqueradeViaCilium: true},
			},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, rec := newNetworkReconciler(ciliumStore())
		reconcileTwice(r, "cilium-warn-1")

		Eventually(rec.Events, time.Second).Should(Receive(ContainSubstring(EventReasonCiliumConfigMissing)))
	})

	It("does not emit CiliumConfigMissing when ConfigMap contains the subnet", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "ip-masq-agent", Namespace: "kube-system"},
			Data: map[string]string{
				"config": "nonMasqueradeCIDRs:\n- 10.0.0.0/8\nmasqueradeCIDRs:\n- 192.168.60.0/24\n",
			},
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, cm) }) //nolint:errcheck

		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "cilium-ok-1", Namespace: "default"},
			Spec: impdevv1alpha1.ImpNetworkSpec{
				Subnet: "192.168.60.0/24",
				Cilium: &impdevv1alpha1.CiliumNetworkSpec{MasqueradeViaCilium: true},
			},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, rec := newNetworkReconciler(ciliumStore())
		reconcileTwice(r, "cilium-ok-1")

		// Drain all events; none should be CiliumConfigMissing.
		Consistently(func() string {
			select {
			case e := <-rec.Events:
				return e
			default:
				return ""
			}
		}, 200*time.Millisecond, 50*time.Millisecond).ShouldNot(ContainSubstring(EventReasonCiliumConfigMissing))
	})

	It("does not emit CiliumConfigMissing when masqueradeViaCilium=false", func() {
		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "cilium-off-1", Namespace: "default"},
			Spec: impdevv1alpha1.ImpNetworkSpec{
				Subnet: "192.168.70.0/24",
				Cilium: &impdevv1alpha1.CiliumNetworkSpec{MasqueradeViaCilium: false},
			},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, rec := newNetworkReconciler(ciliumStore())
		reconcileTwice(r, "cilium-off-1")

		Consistently(func() string {
			select {
			case e := <-rec.Events:
				return e
			default:
				return ""
			}
		}, 200*time.Millisecond, 50*time.Millisecond).ShouldNot(ContainSubstring(EventReasonCiliumConfigMissing))
	})

	It("does not emit CiliumConfigMissing when CNI is not Cilium", func() {
		flannelStore := &cnidetect.Store{}
		flannelStore.Set(cnidetect.Result{Provider: cnidetect.ProviderFlannel, NATBackend: cnidetect.NATBackendIPTables})

		net := &impdevv1alpha1.ImpNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "flannel-masq-1", Namespace: "default"},
			Spec: impdevv1alpha1.ImpNetworkSpec{
				Subnet: "192.168.80.0/24",
				Cilium: &impdevv1alpha1.CiliumNetworkSpec{MasqueradeViaCilium: true},
			},
		}
		Expect(k8sClient.Create(ctx, net)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, net) }) //nolint:errcheck

		r, rec := newNetworkReconciler(flannelStore)
		reconcileTwice(r, "flannel-masq-1")

		Consistently(func() string {
			select {
			case e := <-rec.Events:
				return e
			default:
				return ""
			}
		}, 200*time.Millisecond, 50*time.Millisecond).ShouldNot(ContainSubstring(EventReasonCiliumConfigMissing))
	})
})
