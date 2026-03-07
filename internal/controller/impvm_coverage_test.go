package controller

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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
)

// ─── helpers ────────────────────────────────────────────────────────────────

func readyNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{labelImpEnabled: "true"},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

func fakeRecorder() *record.FakeRecorder { return record.NewFakeRecorder(64) }

// ─── isNodeReady ─────────────────────────────────────────────────────────────

var _ = Describe("isNodeReady", func() {
	It("returns true when Ready=True", func() {
		node := &corev1.Node{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}}}
		Expect(isNodeReady(node)).To(BeTrue())
	})

	It("returns false when Ready=False", func() {
		node := &corev1.Node{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
		}}}
		Expect(isNodeReady(node)).To(BeFalse())
	})

	It("returns false when no Ready condition present", func() {
		node := &corev1.Node{}
		Expect(isNodeReady(node)).To(BeFalse())
	})
})

// ─── conditions ──────────────────────────────────────────────────────────────

var _ = Describe("setScheduled / setUnscheduled", func() {
	It("sets Scheduled condition to True with correct reason", func() {
		vm := &impdevv1alpha1.ImpVM{}
		setScheduled(vm, "node-1")
		c := apimeta.FindStatusCondition(vm.Status.Conditions, ConditionScheduled)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
		Expect(c.Reason).To(Equal("NodeAssigned"))
	})

	It("sets Scheduled condition to False via setUnscheduled", func() {
		vm := &impdevv1alpha1.ImpVM{}
		setUnscheduled(vm)
		c := apimeta.FindStatusCondition(vm.Status.Conditions, ConditionScheduled)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
	})
})

var _ = Describe("setNodeHealthy / setNodeUnhealthy", func() {
	It("sets NodeHealthy=True", func() {
		vm := &impdevv1alpha1.ImpVM{}
		setNodeHealthy(vm)
		c := apimeta.FindStatusCondition(vm.Status.Conditions, ConditionNodeHealthy)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
	})

	It("sets NodeHealthy=False with reason", func() {
		vm := &impdevv1alpha1.ImpVM{}
		setNodeUnhealthy(vm, "node not found")
		c := apimeta.FindStatusCondition(vm.Status.Conditions, ConditionNodeHealthy)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
		Expect(c.Message).To(Equal("node not found"))
	})
})

var _ = Describe("setReadyFromPhase", func() {
	DescribeTable("maps phase to Ready condition",
		func(phase impdevv1alpha1.VMPhase, expectedStatus metav1.ConditionStatus) {
			vm := &impdevv1alpha1.ImpVM{}
			vm.Status.Phase = phase
			setReadyFromPhase(vm)
			c := apimeta.FindStatusCondition(vm.Status.Conditions, ConditionReady)
			Expect(c).NotTo(BeNil())
			Expect(c.Status).To(Equal(expectedStatus))
		},
		Entry("Running → True", impdevv1alpha1.VMPhaseRunning, metav1.ConditionTrue),
		Entry("Failed → False", impdevv1alpha1.VMPhaseFailed, metav1.ConditionFalse),
		Entry("Succeeded → False", impdevv1alpha1.VMPhaseSucceeded, metav1.ConditionFalse),
		Entry("Pending → Unknown", impdevv1alpha1.VMPhasePending, metav1.ConditionUnknown),
	)
})

// ─── countRunningVMs ─────────────────────────────────────────────────────────

var _ = Describe("countRunningVMs", func() {
	It("counts Pending/Running/Starting and skips Failed/Succeeded/Terminating", func() {
		vms := []impdevv1alpha1.ImpVM{
			{Spec: impdevv1alpha1.ImpVMSpec{NodeName: "n1"}, Status: impdevv1alpha1.ImpVMStatus{Phase: impdevv1alpha1.VMPhasePending}},
			{Spec: impdevv1alpha1.ImpVMSpec{NodeName: "n1"}, Status: impdevv1alpha1.ImpVMStatus{Phase: impdevv1alpha1.VMPhaseRunning}},
			{Spec: impdevv1alpha1.ImpVMSpec{NodeName: "n1"}, Status: impdevv1alpha1.ImpVMStatus{Phase: impdevv1alpha1.VMPhaseStarting}},
			{Spec: impdevv1alpha1.ImpVMSpec{NodeName: "n1"}, Status: impdevv1alpha1.ImpVMStatus{Phase: impdevv1alpha1.VMPhaseFailed}},
			{Spec: impdevv1alpha1.ImpVMSpec{NodeName: "n1"}, Status: impdevv1alpha1.ImpVMStatus{Phase: impdevv1alpha1.VMPhaseSucceeded}},
			{Spec: impdevv1alpha1.ImpVMSpec{NodeName: "n1"}, Status: impdevv1alpha1.ImpVMStatus{Phase: impdevv1alpha1.VMPhaseTerminating}},
		}
		counts := countRunningVMs(vms)
		Expect(counts["n1"]).To(Equal(3))
	})
})

// ─── filterByNodeSelector ────────────────────────────────────────────────────

var _ = Describe("filterByNodeSelector", func() {
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "n1", Labels: map[string]string{"zone": "a"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "n2", Labels: map[string]string{"zone": "b"}}},
	}

	It("returns all nodes when selector is empty", func() {
		result := filterByNodeSelector(nodes, nil)
		Expect(result).To(HaveLen(2))
	})

	It("returns matching nodes", func() {
		result := filterByNodeSelector(nodes, map[string]string{"zone": "a"})
		Expect(result).To(HaveLen(1))
		Expect(result[0].Name).To(Equal("n1"))
	})

	It("returns empty when no nodes match", func() {
		result := filterByNodeSelector(nodes, map[string]string{"zone": "c"})
		Expect(result).To(BeEmpty())
	})
})

// ─── resolveHTTPCheck ────────────────────────────────────────────────────────

var _ = Describe("resolveHTTPCheck", func() {
	enabledSpec := &impdevv1alpha1.HTTPCheckSpec{Enabled: true, Port: 8080}
	disabledSpec := &impdevv1alpha1.HTTPCheckSpec{Enabled: false, Port: 8080}

	It("VM spec enabled overrides nil global", func() {
		vm := &impdevv1alpha1.ImpVM{Spec: impdevv1alpha1.ImpVMSpec{
			Probes: &impdevv1alpha1.ProbeSpec{HTTPCheck: enabledSpec},
		}}
		Expect(resolveHTTPCheck(vm, nil)).To(Equal(enabledSpec))
	})

	It("VM spec disabled overrides enabled global", func() {
		vm := &impdevv1alpha1.ImpVM{Spec: impdevv1alpha1.ImpVMSpec{
			Probes: &impdevv1alpha1.ProbeSpec{HTTPCheck: disabledSpec},
		}}
		Expect(resolveHTTPCheck(vm, enabledSpec)).To(BeNil())
	})

	It("nil VM spec falls back to enabled global", func() {
		vm := &impdevv1alpha1.ImpVM{}
		Expect(resolveHTTPCheck(vm, enabledSpec)).To(Equal(enabledSpec))
	})

	It("nil VM spec + disabled global returns nil", func() {
		vm := &impdevv1alpha1.ImpVM{}
		Expect(resolveHTTPCheck(vm, disabledSpec)).To(BeNil())
	})
})

// ─── doHTTPCheck ─────────────────────────────────────────────────────────────

var _ = Describe("doHTTPCheck", func() {
	ctx := context.Background()

	It("returns healthy=true for 200 OK", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		DeferCleanup(srv.Close)

		spec := httpCheckSpecFromServer(srv)
		healthy, msg := doHTTPCheck(ctx, httpHostFromServer(srv), spec)
		Expect(healthy).To(BeTrue())
		Expect(msg).To(Equal("OK"))
	})

	It("returns healthy=false for 500", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		DeferCleanup(srv.Close)

		spec := httpCheckSpecFromServer(srv)
		healthy, msg := doHTTPCheck(ctx, httpHostFromServer(srv), spec)
		Expect(healthy).To(BeFalse())
		Expect(msg).To(Equal("HTTP 500"))
	})

	It("returns healthy=false when connection refused", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		spec := httpCheckSpecFromServer(srv)
		host := httpHostFromServer(srv)
		srv.Close() // close before calling

		healthy, msg := doHTTPCheck(ctx, host, spec)
		Expect(healthy).To(BeFalse())
		Expect(msg).NotTo(BeEmpty())
	})
})

// httpCheckSpecFromServer builds an HTTPCheckSpec pointing at the given test server.
func httpCheckSpecFromServer(srv *httptest.Server) *impdevv1alpha1.HTTPCheckSpec {
	_, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String()) //nolint:errcheck
	port, _ := strconv.Atoi(portStr)                                 //nolint:errcheck
	return &impdevv1alpha1.HTTPCheckSpec{
		Enabled: true,
		Path:    "/healthz",
		Port:    int32(port), //nolint:gosec
	}
}

// httpHostFromServer extracts just the IP/host from the test server address.
func httpHostFromServer(srv *httptest.Server) string {
	host, _, _ := net.SplitHostPort(srv.Listener.Addr().String()) //nolint:errcheck
	return host
}

// ─── applyHTTPCheck ──────────────────────────────────────────────────────────

var _ = Describe("applyHTTPCheck", func() {
	ctx := context.Background()

	makeRunningVM := func(name string, ip string) *impdevv1alpha1.ImpVM {
		return &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Status: impdevv1alpha1.ImpVMStatus{
				Phase: impdevv1alpha1.VMPhaseRunning,
				IP:    ip,
			},
		}
	}

	It("passing check: sets annotation=0 and Ready=True", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		DeferCleanup(srv.Close)

		rec := fakeRecorder()
		r := &ImpVMReconciler{Client: k8sClient, Recorder: rec}
		vm := makeRunningVM("apply-pass", httpHostFromServer(srv))
		spec := httpCheckSpecFromServer(srv)

		changed := r.applyHTTPCheck(ctx, vm, spec)
		Expect(changed).To(BeTrue())
		Expect(vm.Annotations["imp/httpcheck-failures"]).To(Equal("0"))
		c := apimeta.FindStatusCondition(vm.Status.Conditions, ConditionReady)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
	})

	It("failing below threshold: increments annotation, Ready stays Running", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		DeferCleanup(srv.Close)

		rec := fakeRecorder()
		r := &ImpVMReconciler{Client: k8sClient, Recorder: rec}
		spec := httpCheckSpecFromServer(srv)
		spec.FailureThreshold = 3
		vm := makeRunningVM("apply-fail-below", httpHostFromServer(srv))

		r.applyHTTPCheck(ctx, vm, spec)
		Expect(vm.Annotations["imp/httpcheck-failures"]).To(Equal("1"))
		// Not yet at threshold — Ready should not be False.
		c := apimeta.FindStatusCondition(vm.Status.Conditions, ConditionReady)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).NotTo(Equal(metav1.ConditionFalse))
	})

	It("failing at threshold: sets Ready=False and emits event", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		DeferCleanup(srv.Close)

		rec := fakeRecorder()
		r := &ImpVMReconciler{Client: k8sClient, Recorder: rec}
		spec := httpCheckSpecFromServer(srv)
		spec.FailureThreshold = 1
		vm := makeRunningVM("apply-fail-thresh", httpHostFromServer(srv))

		r.applyHTTPCheck(ctx, vm, spec)
		Expect(vm.Annotations["imp/httpcheck-failures"]).To(Equal("1"))
		c := apimeta.FindStatusCondition(vm.Status.Conditions, ConditionReady)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
		Expect(c.Reason).To(Equal("HealthCheckFailed"))
		// Event should have been emitted.
		Expect(rec.Events).To(Receive(ContainSubstring(EventReasonHealthCheckFailed)))
	})

	It("recovery: emits HealthCheckRecovered event when going from failing to healthy", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		DeferCleanup(srv.Close)

		rec := fakeRecorder()
		r := &ImpVMReconciler{Client: k8sClient, Recorder: rec}
		spec := httpCheckSpecFromServer(srv)
		vm := makeRunningVM("apply-recovery", httpHostFromServer(srv))
		// Simulate a previous failure in the annotation.
		vm.Annotations = map[string]string{"imp/httpcheck-failures": "2"}

		r.applyHTTPCheck(ctx, vm, spec)
		Expect(vm.Annotations["imp/httpcheck-failures"]).To(Equal("0"))
		Expect(rec.Events).To(Receive(ContainSubstring(EventReasonHealthCheckRecovered)))
	})

	It("no IP: falls back to phase-derived Ready without mutation", func() {
		rec := fakeRecorder()
		r := &ImpVMReconciler{Client: k8sClient, Recorder: rec}
		vm := makeRunningVM("apply-no-ip", "")
		spec := &impdevv1alpha1.HTTPCheckSpec{Enabled: true, Port: 8080}

		changed := r.applyHTTPCheck(ctx, vm, spec)
		Expect(changed).To(BeFalse())
	})
})

// ─── globalHTTPCheck ─────────────────────────────────────────────────────────

var _ = Describe("globalHTTPCheck", func() {
	ctx := context.Background()

	It("returns nil when no ClusterImpConfig exists", func() {
		r := &ImpVMReconciler{Client: k8sClient, Recorder: fakeRecorder()}
		Expect(r.globalHTTPCheck(ctx)).To(BeNil())
	})

	It("returns spec when ClusterImpConfig 'cluster' with DefaultHttpCheck exists", func() {
		httpSpec := &impdevv1alpha1.HTTPCheckSpec{Enabled: true, Port: 9090}
		cfg := &impdevv1alpha1.ClusterImpConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       impdevv1alpha1.ClusterImpConfigSpec{DefaultHttpCheck: httpSpec},
		}
		Expect(k8sClient.Create(ctx, cfg)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, cfg) }) //nolint:errcheck

		r := &ImpVMReconciler{Client: k8sClient, Recorder: fakeRecorder()}
		result := r.globalHTTPCheck(ctx)
		Expect(result).NotTo(BeNil())
		Expect(result.Port).To(Equal(int32(9090)))
	})
})

// ─── nodeToImpVMs ─────────────────────────────────────────────────────────────

var _ = Describe("nodeToImpVMs", func() {
	ctx := context.Background()

	It("returns only VMs assigned to the given node", func() {
		vm1 := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "n2v-vm1", Namespace: "default",
				Finalizers: []string{finalizerImp}},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: "node-alpha"},
		}
		vm2 := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "n2v-vm2", Namespace: "default",
				Finalizers: []string{finalizerImp}},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: "node-beta"},
		}
		Expect(k8sClient.Create(ctx, vm1)).To(Succeed())
		Expect(k8sClient.Create(ctx, vm2)).To(Succeed())
		DeferCleanup(func() {
			k8sClient.Delete(ctx, vm1) //nolint:errcheck
			k8sClient.Delete(ctx, vm2) //nolint:errcheck
		})

		r := &ImpVMReconciler{Client: k8sClient, Recorder: fakeRecorder()}
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-alpha"}}

		reqs := r.nodeToImpVMs(ctx, node)
		Expect(reqs).To(HaveLen(1))
		Expect(reqs[0].NamespacedName).To(Equal(types.NamespacedName{Name: "n2v-vm1", Namespace: "default"}))
	})
})

// ─── Scheduler: node exists → VM scheduled ───────────────────────────────────

var _ = Describe("ImpVM Scheduler: node exists → scheduled", func() {
	ctx := context.Background()

	It("sets spec.nodeName + phase=Scheduled when a ready node is available", func() {
		node := readyNode("sched-node-1")
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		// Patch status to set Ready condition (status subresource).
		Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, node) }) //nolint:errcheck

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "sched-vm-1", Namespace: "default"},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		r := newReconciler()
		// First reconcile: adds finalizer.
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sched-vm-1", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile: schedules.
		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sched-vm-1", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sched-vm-1", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(Equal("sched-node-1"))
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseScheduled))
	})
})

// ─── Scheduler: node at cap via ClusterImpNodeProfile ────────────────────────

var _ = Describe("ImpVM Scheduler: node at cap", func() {
	ctx := context.Background()

	It("leaves VM Pending when node is at MaxImpVMs capacity", func() {
		node := readyNode("cap-node-1")
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, node) }) //nolint:errcheck

		// Profile caps this node at 1 VM.
		profile := &impdevv1alpha1.ClusterImpNodeProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-node-1"},
			Spec:       impdevv1alpha1.ClusterImpNodeProfileSpec{MaxImpVMs: 1},
		}
		Expect(k8sClient.Create(ctx, profile)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, profile) }) //nolint:errcheck

		// Create an existing Running VM occupying the one slot.
		existingVM := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-existing", Namespace: "default",
				Finalizers: []string{finalizerImp}},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: "cap-node-1"},
		}
		Expect(k8sClient.Create(ctx, existingVM)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, existingVM) }) //nolint:errcheck
		base := existingVM.DeepCopy()
		existingVM.Status.Phase = impdevv1alpha1.VMPhaseRunning
		Expect(k8sClient.Status().Patch(ctx, existingVM, client.MergeFrom(base))).To(Succeed())

		// New VM should not be scheduled.
		newVM := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-new", Namespace: "default"},
		}
		Expect(k8sClient.Create(ctx, newVM)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, newVM) }) //nolint:errcheck

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "cap-new", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "cap-new", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-new", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
		Expect(updated.Spec.NodeName).To(BeEmpty())
	})
})

// ─── handleDeletion with nodeName ────────────────────────────────────────────

var _ = Describe("ImpVM handleDeletion with nodeName", func() {
	ctx := context.Background()

	It("sets phase=Terminating and requeues when nodeName set on deletion", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "del-with-node",
				Namespace:  "default",
				Finalizers: []string{finalizerImp},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: "some-node"},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, vm)).To(Succeed())
		DeferCleanup(func() {
			// Remove finalizer so the object can be GC'd.
			latest := &impdevv1alpha1.ImpVM{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "del-with-node", Namespace: "default"}, latest); err == nil {
				latest.Finalizers = nil
				k8sClient.Update(ctx, latest) //nolint:errcheck
			}
		})

		result, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "del-with-node", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(5 * time.Second))

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "del-with-node", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseTerminating))
	})

	It("force-removes finalizer after termination timeout", func() {
		origTimeout := terminationTimeout
		terminationTimeout = 0 // trigger immediately
		DeferCleanup(func() { terminationTimeout = origTimeout })

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "del-timeout",
				Namespace:  "default",
				Finalizers: []string{finalizerImp},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: "some-node"},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, vm)).To(Succeed())

		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "del-timeout", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Object should be gone (finalizer removed → GC).
		updated := &impdevv1alpha1.ImpVM{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "del-timeout", Namespace: "default"}, updated)
		if err == nil {
			// If still present, finalizer must have been removed.
			Expect(updated.Finalizers).NotTo(ContainElement(finalizerImp))
		}
	})
})

// ─── Scheduler: explicit VCPUCapacity path ───────────────────────────────────

var _ = Describe("ImpVM Scheduler: explicit VCPUCapacity scheduling", func() {
	ctx := context.Background()

	It("schedules VM via explicit-capacity path when ClusterImpNodeProfile has VCPUCapacity set", func() {
		nodeName := "explicit-cap-node"
		node := readyNode(nodeName)
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, node) }) //nolint:errcheck

		profile := &impdevv1alpha1.ClusterImpNodeProfile{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			Spec: impdevv1alpha1.ClusterImpNodeProfileSpec{
				VCPUCapacity: 8,
				MemoryMiB:    8192,
			},
		}
		Expect(k8sClient.Create(ctx, profile)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, profile) }) //nolint:errcheck

		class := &impdevv1alpha1.ImpVMClass{
			ObjectMeta: metav1.ObjectMeta{Name: "explicit-cap-small"},
			Spec: impdevv1alpha1.ImpVMClassSpec{
				VCPU:      2,
				MemoryMiB: 512,
				DiskGiB:   10,
			},
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, class) }) //nolint:errcheck

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "explicit-cap-vm", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "explicit-cap-small"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		r := newReconciler()
		// First reconcile: adds finalizer.
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "explicit-cap-vm", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile: schedules via explicit-capacity path.
		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "explicit-cap-vm", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "explicit-cap-vm", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(Equal(nodeName))
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseScheduled))
	})
})

// ─── nodeIsSchedulable ────────────────────────────────────────────────────────

var _ = Describe("nodeIsSchedulable", func() {
	ready := func(extra ...corev1.NodeCondition) corev1.Node {
		conds := []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
		return corev1.Node{Status: corev1.NodeStatus{Conditions: append(conds, extra...)}}
	}

	It("returns true for a ready node with no pressure", func() {
		Expect(nodeIsSchedulable(ready())).To(BeTrue())
	})

	It("returns false when Spec.Unschedulable is set", func() {
		n := ready()
		n.Spec.Unschedulable = true
		Expect(nodeIsSchedulable(n)).To(BeFalse())
	})

	It("returns false when Ready=False", func() {
		n := corev1.Node{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
		}}}
		Expect(nodeIsSchedulable(n)).To(BeFalse())
	})

	It("returns false when Ready=Unknown", func() {
		n := corev1.Node{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionUnknown},
		}}}
		Expect(nodeIsSchedulable(n)).To(BeFalse())
	})

	It("returns false when no Ready condition is present", func() {
		Expect(nodeIsSchedulable(corev1.Node{})).To(BeFalse())
	})

	It("returns false when MemoryPressure=True", func() {
		Expect(nodeIsSchedulable(ready(corev1.NodeCondition{
			Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue,
		}))).To(BeFalse())
	})

	It("returns false when DiskPressure=True", func() {
		Expect(nodeIsSchedulable(ready(corev1.NodeCondition{
			Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue,
		}))).To(BeFalse())
	})

	It("returns false when PIDPressure=True", func() {
		Expect(nodeIsSchedulable(ready(corev1.NodeCondition{
			Type: corev1.NodePIDPressure, Status: corev1.ConditionTrue,
		}))).To(BeFalse())
	})
})

// ─── filterSchedulable ────────────────────────────────────────────────────────

var _ = Describe("filterSchedulable", func() {
	It("returns only schedulable nodes from a mixed list", func() {
		good := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "good"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			}},
		}
		unschedulable := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "unschedulable"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			}},
		}
		notReady := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "not-ready"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			}},
		}

		result := filterSchedulable([]corev1.Node{good, unschedulable, notReady})
		Expect(result).To(HaveLen(1))
		Expect(result[0].Name).To(Equal("good"))
	})
})

// ─── syncStatus: node healthy path ───────────────────────────────────────────

var _ = Describe("ImpVM syncStatus: node healthy", func() {
	ctx := context.Background()

	It("sets NodeHealthy condition when assigned node is Ready", func() {
		node := readyNode("sync-healthy-node")
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, node) }) //nolint:errcheck

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sync-healthy-vm",
				Namespace:  "default",
				Finalizers: []string{finalizerImp},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: "sync-healthy-node"},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sync-healthy-vm", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sync-healthy-vm", Namespace: "default"}, updated)).To(Succeed())
		c := apimeta.FindStatusCondition(updated.Status.Conditions, ConditionNodeHealthy)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
	})
})
