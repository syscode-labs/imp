# ImpNetwork Controller Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the operator-side `ImpNetworkReconciler` that manages `ImpNetwork` object lifecycle, emits CNI-detection events, and warns when Cilium's ipMasqAgent is not configured for a subnet.

**Architecture:** `ImpNetworkReconciler` lives in `internal/controller/` alongside the existing `ImpVMReconciler`. It holds a `*cnidetect.Store` (populated at startup) and on each reconcile: ensures a finalizer, emits a `CNIDetected` event, checks the Cilium ipMasqAgent ConfigMap when `spec.cilium.masqueradeViaCilium=true`, and sets a `Ready` status condition. The store is passed by pointer from `main.go` where it was created in the previous task.

**Tech Stack:** Go, controller-runtime, Ginkgo/Gomega + envtest (same test harness as the existing ImpVM controller tests in `internal/controller/suite_test.go`).

---

## Key Design Points

- **Finalizer name:** `imp/network-finalizer` (distinct from `imp/finalizer` on ImpVM)
- **Deletion handler:** removes finalizer immediately (agent handles host-side cleanup separately)
- **CiliumConfigMissing check:** reads ConfigMap `kube-system/ip-masq-agent`; checks if `data["config"]` contains the subnet as a substring; warns if absent or missing
- **Events auto-deduplicate** in Kubernetes (same reason+message within 5 min = count++) so emitting on every `sync` call is correct and non-noisy
- **CNI unknown at startup:** if the store has no result yet (unlikely in practice, but possible in tests), treat as `{Provider: ProviderUnknown, NATBackend: NATBackendIPTables}`

---

### Task 1: Add remaining event + condition constants

**Files:**
- Modify: `internal/controller/events.go`

No test needed — constants only.

**Step 1: Add to `events.go`**

Add to the event reason `const` block:
```go
EventReasonBridgeReady        = "BridgeReady"
EventReasonIPAllocated        = "IPAllocated"
EventReasonNATRulesApplied    = "NATRulesApplied"
EventReasonCiliumConfigMissing = "CiliumConfigMissing"
```

Add a new condition type `const` block (after the existing one):
```go
// ImpNetwork condition type constants.
const (
	ConditionNetworkReady = "Ready"
)
```

**Step 2: Build check**
```bash
go build ./internal/controller/...
```
Expected: clean.

**Step 3: Commit**
```bash
git add internal/controller/events.go
git commit -m "feat(controller): add ImpNetwork event reason + condition constants"
```

---

### Task 2: Write failing tests + implement ImpNetworkReconciler core

Core covers: finalizer add, deletion cleanup, `Ready` condition, `CNIDetected` event.

**Files:**
- Create: `internal/controller/impnetwork_controller_test.go`
- Create: `internal/controller/impnetwork_controller.go`

**Step 1: Create the test file**

```go
// internal/controller/impnetwork_controller_test.go
package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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
```

**Step 2: Run to confirm failure**
```bash
go test ./internal/controller/... 2>&1 | head -20
```
Expected: compile error — `ImpNetworkReconciler` and `finalizerImpNetwork` undefined.

**Step 3: Create `internal/controller/impnetwork_controller.go`**

```go
package controller

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
)

const finalizerImpNetwork = "imp/network-finalizer"

// ImpNetworkReconciler reconciles ImpNetwork objects.
type ImpNetworkReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	CNIStore *cnidetect.Store
}

// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

func (r *ImpNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	net := &impdevv1alpha1.ImpNetwork{}
	if err := r.Get(ctx, req.NamespacedName, net); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !net.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, net)
	}

	if !controllerutil.ContainsFinalizer(net, finalizerImpNetwork) {
		controllerutil.AddFinalizer(net, finalizerImpNetwork)
		return ctrl.Result{}, r.Update(ctx, net)
	}

	return r.sync(ctx, net)
}

func (r *ImpNetworkReconciler) handleDeletion(ctx context.Context, net *impdevv1alpha1.ImpNetwork) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(net, finalizerImpNetwork) {
		return ctrl.Result{}, nil
	}
	controllerutil.RemoveFinalizer(net, finalizerImpNetwork)
	return ctrl.Result{}, r.Update(ctx, net)
}

func (r *ImpNetworkReconciler) sync(ctx context.Context, net *impdevv1alpha1.ImpNetwork) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cniResult, _ := r.CNIStore.Result()

	// Emit CNIDetected (or CNIAmbiguous) event to confirm which CNI/NAT is in use.
	if cniResult.Ambiguous {
		r.Recorder.Event(net, corev1.EventTypeWarning, EventReasonCNIAmbiguous,
			"Multiple CNIs detected; NAT backend defaulted to iptables")
	} else {
		r.Recorder.Eventf(net, corev1.EventTypeNormal, EventReasonCNIDetected,
			"CNI: provider=%s natBackend=%s", cniResult.Provider, cniResult.NATBackend)
	}

	// Check Cilium ipMasqAgent config when delegating masquerade to Cilium.
	if net.Spec.Cilium != nil && net.Spec.Cilium.MasqueradeViaCilium {
		isCilium := cniResult.Provider == cnidetect.ProviderCilium ||
			cniResult.Provider == cnidetect.ProviderCiliumKubeProxyFree
		if isCilium && !r.hasCiliumMasqConfig(ctx, net.Spec.Subnet) {
			r.Recorder.Eventf(net, corev1.EventTypeWarning, EventReasonCiliumConfigMissing,
				"Cilium ipMasqAgent not configured for subnet %s — see docs/networking/cilium.md",
				net.Spec.Subnet)
			log.Info("CiliumConfigMissing", "subnet", net.Spec.Subnet)
		}
	}

	// Update status: set Ready condition.
	base := net.DeepCopy()
	setNetworkReady(net)
	if err := r.Status().Patch(ctx, net, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// hasCiliumMasqConfig returns true if the ip-masq-agent ConfigMap in kube-system
// exists and its "config" field contains the given subnet string.
func (r *ImpNetworkReconciler) hasCiliumMasqConfig(ctx context.Context, subnet string) bool {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "ip-masq-agent"}, cm); err != nil {
		if !apierrors.IsNotFound(err) {
			logf.FromContext(ctx).V(1).Info("ip-masq-agent ConfigMap lookup failed", "err", err)
		}
		return false
	}
	return strings.Contains(cm.Data["config"], subnet)
}

// setNetworkReady sets the Ready condition to True on an ImpNetwork.
func setNetworkReady(net *impdevv1alpha1.ImpNetwork) {
	apimeta.SetStatusCondition(&net.Status.Conditions, metav1.Condition{
		Type:               ConditionNetworkReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Reconciled",
		Message:            "ImpNetwork reconciled successfully",
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
}

// SetupWithManager registers the reconciler with the manager.
func (r *ImpNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpNetwork{}).
		Named("impnetwork").
		Complete(r)
}
```

**Step 4: Run the tests**
```bash
go test ./internal/controller/... -v -run "ImpNetwork Controller: core" 2>&1
```
Expected: all 4 specs PASS.

**Step 5: Run the full controller suite to check for regressions**
```bash
go test ./internal/controller/...
```
Expected: all PASS.

**Step 6: Commit**
```bash
git add internal/controller/impnetwork_controller.go internal/controller/impnetwork_controller_test.go
git commit -m "feat(controller): ImpNetworkReconciler — finalizer, Ready condition, CNIDetected event"
```

---

### Task 3: Write failing test + implement CiliumConfigMissing check

The `hasCiliumMasqConfig` method is already implemented above; this task adds the tests that verify the CiliumConfigMissing logic through the full reconcile path.

**Files:**
- Modify: `internal/controller/impnetwork_controller_test.go` (append test cases)

**Step 1: Append Cilium tests to `impnetwork_controller_test.go`**

Add this `Describe` block at the bottom of the file:

```go
var _ = Describe("ImpNetwork Controller: CiliumConfigMissing", func() {
	ctx := context.Background()

	// helper: reconcile twice (once for finalizer, once for sync)
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
```

Note: the `corev1` import is needed. Add it to the test file imports:
```go
corev1 "k8s.io/api/core/v1"
```

**Step 2: Run to confirm tests pass**

The implementation is already in `impnetwork_controller.go` from Task 2, so these tests should pass immediately.

```bash
go test ./internal/controller/... -v -run "CiliumConfigMissing" 2>&1
```
Expected: all 4 specs PASS.

**Step 3: Run full suite**
```bash
go test ./internal/controller/...
```
Expected: all PASS.

**Step 4: Commit**
```bash
git add internal/controller/impnetwork_controller_test.go
git commit -m "test(controller): ImpNetwork CiliumConfigMissing detection tests"
```

---

### Task 4: Register ImpNetworkReconciler in operator main.go

**Files:**
- Modify: `cmd/operator/main.go`

The `cniStore` variable is already declared in `main()`. Pass it to the new reconciler.

**Step 1: Add the registration in `main.go`**

After the existing `ImpVMReconciler` setup block (around line 70), add:

```go
if err = (&controller.ImpNetworkReconciler{
    Client:   mgr.GetClient(),
    Scheme:   mgr.GetScheme(),
    Recorder: mgr.GetEventRecorderFor("impnetwork-controller"),
    CNIStore: cniStore,
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "ImpNetwork")
    os.Exit(1)
}
```

**IMPORTANT:** The `cniStore` variable is declared later in `main()` (before `mgr.Add`). Move the `cniStore` declaration to just after the manager is created (before the first controller setup) so it is in scope when registering the ImpNetworkReconciler.

The new ordering in `main()` should be:
1. `mgr, err := ctrl.NewManager(...)`
2. `cniStore := &cnidetect.Store{}` ← move here
3. `ImpVMReconciler` setup
4. `ImpNetworkReconciler` setup  ← new
5. Webhook registrations
6. Health/ready checks
7. `mgr.Add(&cniDetectRunnable{..., store: cniStore})`
8. `mgr.Start(...)`

**Step 2: Build check**
```bash
go build ./cmd/operator/...
```
Expected: clean.

**Step 3: Run all tests**
```bash
go test ./internal/cnidetect/... ./internal/controller/...
```
Expected: all PASS.

**Step 4: Commit**
```bash
git add cmd/operator/main.go
git commit -m "feat(operator): register ImpNetworkReconciler"
```

---

## Summary

After completing all tasks:

- `internal/controller/impnetwork_controller.go` — full reconciler: finalizer, deletion, `CNIDetected` event, `CiliumConfigMissing` warning, `Ready` condition
- `internal/controller/impnetwork_controller_test.go` — 8 envtest-based specs covering all branches
- `internal/controller/events.go` — `BridgeReady`, `IPAllocated`, `NATRulesApplied`, `CiliumConfigMissing`, `ConditionNetworkReady` constants
- `cmd/operator/main.go` — `ImpNetworkReconciler` registered with the manager

The agent-side work (TAP device, bridge, NAT rules, `BridgeReady`/`IPAllocated`/`NATRulesApplied` events) is the next major task.
