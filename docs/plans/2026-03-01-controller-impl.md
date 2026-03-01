# Controller Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the full ImpVM reconcile loop — scheduler, status sync, node health, deletion, and HTTP probe.

**Architecture:** Thin reconcile loop in `impvm_controller.go` delegating to `impvm_scheduler.go`, `impvm_health.go`, and `impvm_conditions.go`. Operator owns `status.conditions`; agent owns `status.phase`, `status.ip`, `status.runtimePID`, `status.nodeName`. See `docs/plans/2026-03-01-controller-design.md` for full design.

**Tech Stack:** Go 1.25, controller-runtime v0.23, Ginkgo v2 + Gomega, envtest.

---

## Task 1: API changes

Required before anything else — types must exist before controller code.

**Files:**
- Modify: `api/v1alpha1/shared_types.go`
- Modify: `api/v1alpha1/impvm_types.go`
- Modify: `api/v1alpha1/clusterimpconfig_types.go`
- Auto-regenerated: `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/bases/`

**Step 1: Replace VMPhase enum in `api/v1alpha1/shared_types.go`**

Replace:
```go
// VMPhase is the current lifecycle phase of an ImpVM.
// +kubebuilder:validation:Enum=Pending;Scheduling;Starting;Running;Stopping;Stopped;Failed
type VMPhase string

const (
	VMPhasePending    VMPhase = "Pending"
	VMPhaseScheduling VMPhase = "Scheduling"
	VMPhaseStarting   VMPhase = "Starting"
	VMPhaseRunning    VMPhase = "Running"
	VMPhaseStopping   VMPhase = "Stopping"
	VMPhaseStopped    VMPhase = "Stopped"
	VMPhaseFailed     VMPhase = "Failed"
)
```
With:
```go
// VMPhase is the current lifecycle phase of an ImpVM.
// +kubebuilder:validation:Enum=Pending;Scheduled;Starting;Running;Terminating;Succeeded;Failed
type VMPhase string

const (
	VMPhasePending     VMPhase = "Pending"
	VMPhaseScheduled   VMPhase = "Scheduled"
	VMPhaseStarting    VMPhase = "Starting"
	VMPhaseRunning     VMPhase = "Running"
	VMPhaseTerminating VMPhase = "Terminating"
	VMPhaseSucceeded   VMPhase = "Succeeded"
	VMPhaseFailed      VMPhase = "Failed"
)
```

**Step 2: Add `HTTPCheckSpec` to `api/v1alpha1/shared_types.go`**

Append to the bottom of the file:
```go
// HTTPCheckSpec configures the operator-side HTTP health check (opt-in).
// Enabled per-VM via spec.probes.httpCheck or cluster-wide via ClusterImpConfig.spec.defaultHttpCheck.
type HTTPCheckSpec struct {
	// Enabled turns on the operator HTTP health check.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Path is the HTTP path to GET. Defaults to /healthz.
	// +optional
	// +kubebuilder:default=/healthz
	Path string `json:"path,omitempty"`
	// Port is the TCP port to check.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
	// IntervalSeconds is how often the operator checks the endpoint.
	// +optional
	// +kubebuilder:default=10
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`
	// FailureThreshold is the number of consecutive failures before marking Ready=False.
	// +optional
	// +kubebuilder:default=3
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}
```

**Step 3: Add `HTTPCheck` field to `ProbeSpec` in `api/v1alpha1/shared_types.go`**

Change:
```go
type ProbeSpec struct {
	StartupProbe   *Probe `json:"startupProbe,omitempty"`
	ReadinessProbe *Probe `json:"readinessProbe,omitempty"`
	LivenessProbe  *Probe `json:"livenessProbe,omitempty"`
}
```
To:
```go
type ProbeSpec struct {
	StartupProbe   *Probe         `json:"startupProbe,omitempty"`
	ReadinessProbe *Probe         `json:"readinessProbe,omitempty"`
	LivenessProbe  *Probe         `json:"livenessProbe,omitempty"`
	// HTTPCheck configures an optional operator-driven HTTP health check (opt-in).
	// +optional
	HTTPCheck *HTTPCheckSpec `json:"httpCheck,omitempty"`
}
```

**Step 4: Rename `FirecrackerPID` → `RuntimePID` in `api/v1alpha1/impvm_types.go`**

Change:
```go
// FirecrackerPID is the PID of the Firecracker process on the node (informational).
// +optional
FirecrackerPID int64 `json:"firecrackerPID,omitempty"`
```
To:
```go
// RuntimePID is the PID of the VM runtime process on the node (informational).
// +optional
RuntimePID int64 `json:"runtimePID,omitempty"`
```

**Step 5: Add `DefaultHttpCheck` to `ClusterImpConfigSpec` in `api/v1alpha1/clusterimpconfig_types.go`**

Add field to `ClusterImpConfigSpec`:
```go
// DefaultHttpCheck sets the cluster-wide default for the operator HTTP health check.
// Individual ImpVMs can override this per-VM via spec.probes.httpCheck.
// Disabled by default.
// +optional
DefaultHttpCheck *HTTPCheckSpec `json:"defaultHttpCheck,omitempty"`
```

**Step 6: Regenerate deepcopy and CRD manifests**

Run: `make generate manifests`
Expected: exits 0; `zz_generated.deepcopy.go` updated; `config/crd/bases/*.yaml` updated.

**Step 7: Fix any test references to old field names**

Run: `grep -r "firecrackerPID\|FirecrackerPID\|VMPhaseScheduling\|VMPhaseStopping\|VMPhaseStopped" .`
Update any references found to use the new names. (`VMPhaseScheduling` → `VMPhaseScheduled`, `VMPhaseStopping`/`VMPhaseStopped` → `VMPhaseTerminating`/`VMPhaseSucceeded`)

**Step 8: Run tests**

Run: `make test`
Expected: PASS

**Step 9: Commit**

```bash
git add api/v1alpha1/ config/crd/
git commit -m "feat: API changes — HTTPCheckSpec, runtimePID, Scheduled/Terminating phases"
```

---

## Task 2: Event constants and condition helpers

**Files:**
- Create: `internal/controller/events.go`
- Create: `internal/controller/impvm_conditions.go`

**Step 1: Create `internal/controller/events.go`**

```go
package controller

// Event reason constants emitted on ImpVM objects.
const (
	EventReasonScheduled          = "Scheduled"
	EventReasonUnschedulable      = "Unschedulable"
	EventReasonNodeLost           = "NodeLost"
	EventReasonRescheduling       = "Rescheduling"
	EventReasonTerminating        = "Terminating"
	EventReasonTerminationTimeout = "TerminationTimeout"
	EventReasonHealthCheckFailed  = "HealthCheckFailed"
	EventReasonHealthCheckRecovered = "HealthCheckRecovered"
)

// Condition type constants.
const (
	ConditionScheduled   = "Scheduled"
	ConditionReady       = "Ready"
	ConditionNodeHealthy = "NodeHealthy"
)
```

**Step 2: Create `internal/controller/impvm_conditions.go`**

```go
package controller

import (
	"time"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func setCondition(vm *impdevv1alpha1.ImpVM, condType string, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
}

func setScheduled(vm *impdevv1alpha1.ImpVM, nodeName string) {
	setCondition(vm, ConditionScheduled, metav1.ConditionTrue, "NodeAssigned",
		"VM scheduled to node "+nodeName)
}

func setUnscheduled(vm *impdevv1alpha1.ImpVM) {
	setCondition(vm, ConditionScheduled, metav1.ConditionFalse, "NoNodeAvailable",
		"No eligible node with available capacity")
}

func setNodeHealthy(vm *impdevv1alpha1.ImpVM) {
	setCondition(vm, ConditionNodeHealthy, metav1.ConditionTrue, "NodeReady", "Assigned node is Ready")
}

func setNodeUnhealthy(vm *impdevv1alpha1.ImpVM, reason string) {
	setCondition(vm, ConditionNodeHealthy, metav1.ConditionFalse, "NodeNotReady", reason)
}

func setReadyFromPhase(vm *impdevv1alpha1.ImpVM) {
	switch vm.Status.Phase {
	case impdevv1alpha1.VMPhaseRunning:
		setCondition(vm, ConditionReady, metav1.ConditionTrue, "Running", "VM is running")
	case impdevv1alpha1.VMPhaseFailed, impdevv1alpha1.VMPhaseSucceeded:
		setCondition(vm, ConditionReady, metav1.ConditionFalse, string(vm.Status.Phase), "VM is not running")
	default:
		setCondition(vm, ConditionReady, metav1.ConditionUnknown, "Waiting", "Waiting for VM to start")
	}
}
```

**Step 3: Compile check**

Run: `go build ./internal/controller/`
Expected: exits 0

**Step 4: Run tests**

Run: `make test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/controller/events.go internal/controller/impvm_conditions.go
git commit -m "feat: event constants and condition helpers"
```

---

## Task 3: Scheduler

**Files:**
- Create: `internal/controller/impvm_scheduler.go`
- Modify: `internal/controller/impvm_controller.go`
- Modify: `internal/controller/impvm_controller_test.go`
- Modify: `cmd/operator/main.go`

**Step 1: Add RBAC markers to `impvm_controller.go`**

Add these lines directly above the `Reconcile` function (after existing `+kubebuilder:rbac` markers):
```go
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=clusterimpnodeprofiles,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
```

**Step 2: Add `Recorder` field to `ImpVMReconciler`**

In `internal/controller/impvm_controller.go`, change the struct:
```go
import "k8s.io/client-go/tools/record"

type ImpVMReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}
```

**Step 3: Wire `Recorder` in `cmd/operator/main.go`**

Find the `ImpVMReconciler{}` setup block (search for `ImpVMReconciler`) and add `Recorder`:
```go
if err = (&controller.ImpVMReconciler{
    Client:   mgr.GetClient(),
    Scheme:   mgr.GetScheme(),
    Recorder: mgr.GetEventRecorderFor("impvm-controller"),
}).SetupWithManager(mgr); err != nil {
```

**Step 4: Write the scheduler test (failing)**

Replace the existing body of `internal/controller/impvm_controller_test.go` with:
```go
package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newReconciler() *ImpVMReconciler {
	return &ImpVMReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(32),
	}
}

var _ = Describe("ImpVM Scheduler", func() {
	ctx := context.Background()

	It("sets phase=Pending and emits Unschedulable when no imp/enabled nodes exist", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "sched-no-nodes", Namespace: "default"},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) })

		// First reconcile: adds finalizer and returns
		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sched-no-nodes", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile: tries to schedule, finds no nodes
		_, err = newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sched-no-nodes", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sched-no-nodes", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})
})

var _ = Describe("ImpVM SyncStatus", func() {
	ctx := context.Background()

	It("clears nodeName and sets Pending for ephemeral VM when assigned node not found", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sync-no-node",
				Namespace:  "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{
				NodeName:  "ghost-node",
				Lifecycle: impdevv1alpha1.VMLifecycleEphemeral,
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) })

		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sync-no-node", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sync-no-node", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})
})

var _ = Describe("ImpVM Deletion", func() {
	ctx := context.Background()

	It("removes finalizer immediately when spec.nodeName is empty on deletion", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "del-unscheduled",
				Namespace:  "default",
				Finalizers: []string{"imp/finalizer"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, vm)).To(Succeed())

		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "del-unscheduled", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "del-unscheduled", Namespace: "default"}, updated)
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})
})
```

**Step 5: Run tests to verify they fail**

Run: `go test ./internal/controller/ -v 2>&1 | tail -20`
Expected: FAIL (scheduler not yet implemented)

**Step 6: Create `internal/controller/impvm_scheduler.go`**

```go
package controller

import (
	"context"
	"sort"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const labelImpEnabled = "imp/enabled"

// schedule selects a node for vm. Returns "" if no suitable node exists.
func (r *ImpVMReconciler) schedule(ctx context.Context, vm *impdevv1alpha1.ImpVM) (string, error) {
	// 1. List nodes with imp/enabled=true
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.MatchingLabels{labelImpEnabled: "true"}); err != nil {
		return "", err
	}

	// 2. Filter by spec.nodeSelector
	eligible := filterByNodeSelector(nodeList.Items, vm.Spec.NodeSelector)
	if len(eligible) == 0 {
		return "", nil
	}

	// 3. Count running VMs per node
	allVMs := &impdevv1alpha1.ImpVMList{}
	if err := r.List(ctx, allVMs); err != nil {
		return "", err
	}
	runningPerNode := countRunningVMs(allVMs.Items)

	// 4. Apply capacity cap from ClusterImpNodeProfile (if present)
	type candidate struct {
		name    string
		running int
	}
	var candidates []candidate
	for _, node := range eligible {
		profile := &impdevv1alpha1.ClusterImpNodeProfile{}
		err := r.Get(ctx, client.ObjectKey{Name: node.Name}, profile)
		if err != nil {
			// No profile → no hard cap
			candidates = append(candidates, candidate{name: node.Name, running: runningPerNode[node.Name]})
			continue
		}
		if profile.Spec.MaxImpVMs > 0 && int32(runningPerNode[node.Name]) >= profile.Spec.MaxImpVMs {
			continue // at capacity
		}
		candidates = append(candidates, candidate{name: node.Name, running: runningPerNode[node.Name]})
	}

	if len(candidates) == 0 {
		return "", nil
	}

	// 5. Least-loaded first; alphabetical tie-break
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].running != candidates[j].running {
			return candidates[i].running < candidates[j].running
		}
		return candidates[i].name < candidates[j].name
	})
	return candidates[0].name, nil
}

func filterByNodeSelector(nodes []corev1.Node, selector map[string]string) []corev1.Node {
	if len(selector) == 0 {
		return nodes
	}
	var result []corev1.Node
	for _, node := range nodes {
		match := true
		for k, v := range selector {
			if node.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			result = append(result, node)
		}
	}
	return result
}

// countRunningVMs counts VMs per node where spec.nodeName != "" and phase != Failed.
func countRunningVMs(vms []impdevv1alpha1.ImpVM) map[string]int {
	counts := make(map[string]int)
	for _, vm := range vms {
		if vm.Spec.NodeName != "" && vm.Status.Phase != impdevv1alpha1.VMPhaseFailed {
			counts[vm.Spec.NodeName]++
		}
	}
	return counts
}
```

**Step 7: Implement the full `Reconcile` loop in `impvm_controller.go`**

Replace the stub `Reconcile` function with:

```go
package controller

import (
	"context"
	"time"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const finalizerImp = "imp/finalizer"
const terminationTimeout = 2 * time.Minute

// ImpVMReconciler reconciles ImpVM objects.
type ImpVMReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impvms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvms/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=clusterimpnodeprofiles,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=clusterimpconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *ImpVMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	vm := &impdevv1alpha1.ImpVM{}
	if err := r.Get(ctx, req.NamespacedName, vm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 1. Handle deletion
	if !vm.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, vm)
	}

	// 2. Ensure finalizer
	if !controllerutil.ContainsFinalizer(vm, finalizerImp) {
		controllerutil.AddFinalizer(vm, finalizerImp)
		return ctrl.Result{}, r.Update(ctx, vm)
	}

	// 3. Schedule if not yet assigned
	if vm.Spec.NodeName == "" {
		nodeName, err := r.schedule(ctx, vm)
		if err != nil {
			return ctrl.Result{}, err
		}
		if nodeName == "" {
			log.Info("no node available", "vm", vm.Name)
			vmCopy := vm.DeepCopy()
			vm.Status.Phase = impdevv1alpha1.VMPhasePending
			setUnscheduled(vm)
			if err := r.Status().Patch(ctx, vm, client.MergeFrom(vmCopy)); err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Event(vm, corev1.EventTypeWarning, EventReasonUnschedulable,
				"No eligible node with available capacity")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		// Assign to node
		specPatch := client.MergeFrom(vm.DeepCopy())
		vm.Spec.NodeName = nodeName
		if err := r.Patch(ctx, vm, specPatch); err != nil {
			return ctrl.Result{}, err
		}
		vmCopy := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseScheduled
		setScheduled(vm, nodeName)
		if err := r.Status().Patch(ctx, vm, client.MergeFrom(vmCopy)); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(vm, corev1.EventTypeNormal, EventReasonScheduled, "VM scheduled to node "+nodeName)
		return ctrl.Result{}, nil
	}

	// 4. SyncStatus
	return r.syncStatus(ctx, vm)
}

func (r *ImpVMReconciler) syncStatus(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	node := &corev1.Node{}
	err := r.Get(ctx, client.ObjectKey{Name: vm.Spec.NodeName}, node)
	nodeHealthy := err == nil && isNodeReady(node)

	if !nodeHealthy {
		reason := "node not found"
		if err == nil {
			reason = "node is not Ready"
		}
		vmCopy := vm.DeepCopy()
		setNodeUnhealthy(vm, reason)

		if vm.Spec.Lifecycle == impdevv1alpha1.VMLifecycleEphemeral {
			specPatch := client.MergeFrom(vm.DeepCopy())
			vm.Spec.NodeName = ""
			if err2 := r.Patch(ctx, vm, specPatch); err2 != nil {
				return ctrl.Result{}, err2
			}
			vm.Status.Phase = impdevv1alpha1.VMPhasePending
			setUnscheduled(vm)
			if err2 := r.Status().Patch(ctx, vm, client.MergeFrom(vmCopy)); err2 != nil {
				return ctrl.Result{}, err2
			}
			r.Recorder.Event(vm, corev1.EventTypeNormal, EventReasonRescheduling,
				"Ephemeral VM rescheduled after node loss")
			return ctrl.Result{}, nil
		}
		// Persistent → fail
		vm.Status.Phase = impdevv1alpha1.VMPhaseFailed
		if err2 := r.Status().Patch(ctx, vm, client.MergeFrom(vmCopy)); err2 != nil {
			return ctrl.Result{}, err2
		}
		r.Recorder.Event(vm, corev1.EventTypeWarning, EventReasonNodeLost,
			"Assigned node lost; persistent VM marked Failed")
		return ctrl.Result{}, nil
	}

	// Node healthy — derive conditions
	vmCopy := vm.DeepCopy()
	setNodeHealthy(vm)
	setScheduled(vm, vm.Spec.NodeName)

	// Apply HTTP check before setReadyFromPhase so it can override Ready condition
	if vm.Status.Phase == impdevv1alpha1.VMPhaseRunning {
		httpSpec := resolveHTTPCheck(vm, r.globalHTTPCheck(ctx))
		if httpSpec != nil {
			r.applyHTTPCheck(ctx, vm, httpSpec)
		} else {
			setReadyFromPhase(vm)
		}
	} else {
		setReadyFromPhase(vm)
	}

	if err2 := r.Status().Patch(ctx, vm, client.MergeFrom(vmCopy)); err2 != nil {
		return ctrl.Result{}, err2
	}

	if vm.Status.Phase == impdevv1alpha1.VMPhaseRunning {
		return ctrl.Result{}, nil // watch-driven in steady state
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *ImpVMReconciler) handleDeletion(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(vm, finalizerImp) {
		return ctrl.Result{}, nil
	}

	// Agent already cleaned up (cleared spec.nodeName + set Succeeded)
	if vm.Spec.NodeName == "" {
		controllerutil.RemoveFinalizer(vm, finalizerImp)
		return ctrl.Result{}, r.Update(ctx, vm)
	}

	// Check for termination timeout
	deadline := vm.DeletionTimestamp.Add(terminationTimeout)
	if time.Now().After(deadline) {
		r.Recorder.Event(vm, corev1.EventTypeWarning, EventReasonTerminationTimeout,
			"Finalizer force-removed after 2min termination timeout")
		controllerutil.RemoveFinalizer(vm, finalizerImp)
		return ctrl.Result{}, r.Update(ctx, vm)
	}

	// Signal agent by setting phase=Terminating
	if vm.Status.Phase != impdevv1alpha1.VMPhaseTerminating {
		vmCopy := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseTerminating
		if err := r.Status().Patch(ctx, vm, client.MergeFrom(vmCopy)); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(vm, corev1.EventTypeNormal, EventReasonTerminating,
			"Waiting for agent to stop VM")
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *ImpVMReconciler) globalHTTPCheck(ctx context.Context) *impdevv1alpha1.HTTPCheckSpec {
	cfg := &impdevv1alpha1.ClusterImpConfig{}
	if err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); err != nil {
		return nil
	}
	return cfg.Spec.DefaultHttpCheck
}

func isNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImpVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpVM{}).
		Named("impvm").
		Complete(r)
}
```

Note: Watches for Nodes are added in Task 8 to keep this task focused.

**Step 8: Compile check**

Run: `go build ./...`
Expected: exits 0. Fix any import errors.

**Step 9: Run tests to verify they pass**

Run: `go test ./internal/controller/ -v 2>&1 | tail -30`
Expected: all three Describe blocks PASS

**Step 10: Run all tests**

Run: `make test`
Expected: PASS

**Step 11: Commit**

```bash
git add internal/controller/ cmd/operator/
git commit -m "feat: scheduler, status sync, deletion — core reconcile loop"
```

---

## Task 4: HTTP health check

**Files:**
- Create: `internal/controller/impvm_health.go`

**Step 1: Create `internal/controller/impvm_health.go`**

```go
package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// resolveHTTPCheck returns the effective HTTPCheckSpec for vm (VM spec overrides global default).
// Returns nil if the HTTP check is disabled for this VM.
func resolveHTTPCheck(vm *impdevv1alpha1.ImpVM, globalDefault *impdevv1alpha1.HTTPCheckSpec) *impdevv1alpha1.HTTPCheckSpec {
	if vm.Spec.Probes != nil && vm.Spec.Probes.HTTPCheck != nil {
		if vm.Spec.Probes.HTTPCheck.Enabled {
			return vm.Spec.Probes.HTTPCheck
		}
		return nil // explicitly disabled at VM level
	}
	if globalDefault != nil && globalDefault.Enabled {
		return globalDefault
	}
	return nil
}

// applyHTTPCheck runs the HTTP probe and updates the Ready condition + emits events.
// Failure count is tracked in the annotation "imp/httpcheck-failures".
func (r *ImpVMReconciler) applyHTTPCheck(ctx context.Context, vm *impdevv1alpha1.ImpVM, spec *impdevv1alpha1.HTTPCheckSpec) {
	if vm.Status.IP == "" {
		setReadyFromPhase(vm) // no IP yet, fall back to phase-derived Ready
		return
	}

	healthy, msg := doHTTPCheck(ctx, vm.Status.IP, spec)

	threshold := spec.FailureThreshold
	if threshold == 0 {
		threshold = 3
	}

	if vm.Annotations == nil {
		vm.Annotations = make(map[string]string)
	}

	if healthy {
		wasFailure := vm.Annotations["imp/httpcheck-failures"] != "" &&
			vm.Annotations["imp/httpcheck-failures"] != "0"
		if wasFailure {
			r.Recorder.Event(vm, corev1.EventTypeNormal, EventReasonHealthCheckRecovered,
				"HTTP probe passing again")
		}
		vm.Annotations["imp/httpcheck-failures"] = "0"
		setCondition(vm, ConditionReady, metav1.ConditionTrue, "Running", "VM is running and HTTP probe passing")
		return
	}

	var failures int32
	fmt.Sscan(vm.Annotations["imp/httpcheck-failures"], &failures) //nolint:errcheck
	failures++
	vm.Annotations["imp/httpcheck-failures"] = fmt.Sprintf("%d", failures)

	if failures >= threshold {
		setCondition(vm, ConditionReady, metav1.ConditionFalse, "HealthCheckFailed", msg)
		r.Recorder.Event(vm, corev1.EventTypeWarning, EventReasonHealthCheckFailed,
			fmt.Sprintf("HTTP probe failed %d consecutive times: %s", failures, msg))
	} else {
		setReadyFromPhase(vm) // not yet at threshold
	}
}

// doHTTPCheck performs a single HTTP GET. Returns (healthy, message).
func doHTTPCheck(ctx context.Context, ip string, spec *impdevv1alpha1.HTTPCheckSpec) (bool, string) {
	path := spec.Path
	if path == "" {
		path = "/healthz"
	}
	url := fmt.Sprintf("http://%s:%d%s", ip, spec.Port, path)

	timeout := time.Duration(spec.IntervalSeconds) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	hc := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := hc.Do(req) //nolint:gosec
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, "OK"
	}
	return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
}
```

**Step 2: Compile check**

Run: `go build ./...`
Expected: exits 0

**Step 3: Run all tests**

Run: `make test`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/controller/impvm_health.go
git commit -m "feat: operator HTTP health check — opt-in, per-VM and global config"
```

---

## Task 5: Node watch + label cleanup

**Files:**
- Modify: `internal/controller/impvm_controller.go`
- Modify: `config/manager/manager.yaml`
- Modify: `AGENTS.md`

**Step 1: Add Node watch to `SetupWithManager`**

In `internal/controller/impvm_controller.go`, replace `SetupWithManager`:
```go
func (r *ImpVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpVM{}).
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToImpVMs),
		).
		Named("impvm").
		Complete(r)
}

// nodeToImpVMs maps a Node event to all ImpVMs assigned to that node.
func (r *ImpVMReconciler) nodeToImpVMs(ctx context.Context, obj client.Object) []reconcile.Request {
	allVMs := &impdevv1alpha1.ImpVMList{}
	if err := r.List(ctx, allVMs); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, vm := range allVMs.Items {
		if vm.Spec.NodeName == obj.GetName() {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: vm.Name, Namespace: vm.Namespace},
			})
		}
	}
	return reqs
}
```

Add imports:
```go
"k8s.io/apimachinery/pkg/types"
"sigs.k8s.io/controller-runtime/pkg/handler"
"sigs.k8s.io/controller-runtime/pkg/reconcile"
```

**Step 2: Fix `config/manager/manager.yaml` labels**

Replace all three occurrences of `app.kubernetes.io/name: go-scaffold` with `app.kubernetes.io/name: imp`.

**Step 3: Fix `AGENTS.md` title**

Change line 1:
- `# go-scaffold - AI Agent Guide` → `# imp - AI Agent Guide`

**Step 4: Regenerate RBAC manifests**

Run: `make manifests`
Expected: exits 0; `config/rbac/role.yaml` updated with new RBAC markers.

**Step 5: Run all tests**

Run: `make test`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/controller/impvm_controller.go config/manager/manager.yaml AGENTS.md config/rbac/
git commit -m "feat: Node watch for reactive reconcile; rename go-scaffold → imp labels"
```
