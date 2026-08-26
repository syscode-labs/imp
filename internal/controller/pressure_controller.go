/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// Pressure-suspension annotations. The first records that this controller —
// not a human — moved the VM to Suspended, so nothing auto-resumes it and
// operators can find affected VMs; the second preserves the prior desired
// state for manual restoration.
const (
	annotationPressureSuspended = "imp.dev/pressure-suspended"
	annotationPressureRestore   = "imp.dev/pressure-restore-state"

	pressureSweepRequest = "pressure-sweep"

	pressureRequeueInterval = 30 * time.Second
)

// PressureSuspensionMeter counts suspension actions per node; wired by
// InitMetrics when the operator starts with pressure lifecycle enabled.
var PressureSuspensionMeter interface {
	Add(ctx context.Context, node string)
}

type otelPressureMeter struct{ counter metric.Int64Counter }

func (m otelPressureMeter) Add(ctx context.Context, node string) {
	m.counter.Add(ctx, 1, metric.WithAttributes(attribute.String("node", node)))
}

// InitPressureMetrics registers the suspension counter. Called from main()
// only when the pressure lifecycle is enabled.
func InitPressureMetrics(meter metric.Meter) error {
	c, err := meter.Int64Counter(
		"imp_pressure_suspensions_total",
		metric.WithDescription("ImpVMs suspended by the pressure lifecycle controller."),
	)
	if err != nil {
		return err
	}
	PressureSuspensionMeter = otelPressureMeter{counter: c}
	return nil
}

// PressureReconciler suspends ImpVMs on nodes reporting MemoryPressure when
// enabled. It is opt-in via helm value pressureLifecycle.enabled; when
// disabled Reconcile is never registered.
type PressureReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// SetupWithManager registers a node watch mapped to a single sweep request.
func (r *PressureReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("pressure-lifecycle").
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: pressureSweepRequest}}}
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

// Reconcile performs one sweep: for every imp-enabled node currently in
// MemoryPressure, suspend at most one victim. One suspension per tick keeps
// each action observable and lets freed memory register on Node conditions
// before the next decision.
func (r *PressureReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.MatchingLabels{labelImpEnabled: "true"}); err != nil {
		return ctrl.Result{}, err
	}

	act := false
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if !nodeHasMemoryPressure(node) {
			continue
		}
		victim, err := r.electVictim(ctx, node.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		if victim == nil {
			log.Info("Node in MemoryPressure but no safe ImpVM victim", "node", node.Name)
			continue
		}
		if err := r.suspendVictim(ctx, node.Name, victim); err != nil {
			return ctrl.Result{}, err
		}
		act = true
	}

	if act {
		return ctrl.Result{RequeueAfter: pressureRequeueInterval}, nil
	}
	return ctrl.Result{}, nil
}

// nodeHasMemoryPressure reports whether the node's MemoryPressure condition
// is True.
func nodeHasMemoryPressure(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeMemoryPressure && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// electVictim picks the next suspension candidate on a pressured node:
// largest class memory first, oldest creation as tie-break, warm-pool members
// deprioritized. Returns nil when no safe victim exists.
func (r *PressureReconciler) electVictim(ctx context.Context, nodeName string) (*impdevv1alpha1.ImpVM, error) {
	list := &impdevv1alpha1.ImpVMList{}
	if err := r.List(ctx, list); err != nil {
		return nil, err
	}

	type scored struct {
		vm     *impdevv1alpha1.ImpVM
		memMiB int32
		warm   bool
	}
	var cands []scored
	for i := range list.Items {
		vm := &list.Items[i]
		if vm.Spec.NodeName != nodeName || !isSafePressureVictim(vm) {
			continue
		}
		mem := int32(0)
		if spec, err := resolveClassSpec(ctx, r.Client, vm); err == nil {
			mem = spec.MemoryMiB
		}
		_, warm := vm.Labels[impdevv1alpha1.LabelWarmPool]
		cands = append(cands, scored{vm: vm, memMiB: mem, warm: warm})
	}
	if len(cands) == 0 {
		return nil, nil
	}

	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.warm != b.warm {
			return !a.warm // non-warm-pool victims first
		}
		if a.memMiB != b.memMiB {
			return a.memMiB > b.memMiB // biggest memory freed first
		}
		return a.vm.CreationTimestamp.Before(&b.vm.CreationTimestamp) // oldest first
	})

	return cands[0].vm, nil
}

// isSafePressureVictim filters to Running VMs with a plain Running desired
// state that this controller has not already suspended and that are not
// flagged mid-snapshot/migration by their owning controllers.
func isSafePressureVictim(vm *impdevv1alpha1.ImpVM) bool {
	if vm.Status.Phase != impdevv1alpha1.VMPhaseRunning {
		return false
	}
	if vm.Spec.DesiredState != impdevv1alpha1.VMDesiredStateRunning {
		return false // ScaleToZero/Suspended manage themselves
	}
	if _, handled := vm.Annotations[annotationPressureSuspended]; handled {
		return false
	}
	for _, busyAnno := range []string{"imp.dev/snapshot-in-progress", "imp.dev/migration-in-progress"} {
		if _, busy := vm.Annotations[busyAnno]; busy {
			return false
		}
	}
	return true
}

// suspendVictim flips DesiredState to Suspended (the agent snapshots to disk,
// stops Firecracker and frees memory), marks the VM so nothing auto-resumes
// it, records the prior state for manual restore, emits an Event and bumps
// the metric.
func (r *PressureReconciler) suspendVictim(ctx context.Context, nodeName string, vm *impdevv1alpha1.ImpVM) error {
	base := vm.DeepCopy()
	if vm.Annotations == nil {
		vm.Annotations = map[string]string{}
	}
	vm.Annotations[annotationPressureRestore] = string(impdevv1alpha1.VMDesiredStateRunning)
	vm.Annotations[annotationPressureSuspended] = "from-running"
	vm.Spec.DesiredState = impdevv1alpha1.VMDesiredStateSuspended
	if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return err
	}

	r.emitEvent(vm, "SuspendedDueToNodeMemoryPressure",
		fmt.Sprintf("Node %s entered MemoryPressure; suspending ImpVM (largest-memory-first policy). "+
			"Resume manually: kubectl annotate impvm %s -n %s %s- && kubectl patch impvm %s -n %s --subresource=status --type=merge -p='{\\\"status\\\":{\\\"phase\\\":\\\"Resuming\\\"}}'",
			nodeName, vm.Name, vm.Namespace, annotationPressureSuspended, vm.Name, vm.Namespace))

	if PressureSuspensionMeter != nil {
		PressureSuspensionMeter.Add(ctx, nodeName)
	}
	logf.FromContext(ctx).Info("Suspended ImpVM due to node MemoryPressure",
		"vm", vm.Namespace+"/"+vm.Name, "node", nodeName)
	return nil
}

// emitEvent records through the recorder when available (tests pass nil).
func (r *PressureReconciler) emitEvent(obj runtime.Object, reason, msg string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(obj, corev1.EventTypeNormal, reason, "%s", msg)
}
