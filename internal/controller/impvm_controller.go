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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
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
	log := logf.FromContext(ctx)

	node := &corev1.Node{}
	err := r.Get(ctx, client.ObjectKey{Name: vm.Spec.NodeName}, node)
	if err != nil && !apierrors.IsNotFound(err) {
		// Transient API error — let controller-runtime retry with backoff.
		return ctrl.Result{}, err
	}
	nodeHealthy := err == nil && isNodeReady(node)

	if !nodeHealthy {
		reason := "node not found"
		if err == nil {
			reason = "node is not Ready"
		}
		log.Info("assigned node unhealthy", "node", vm.Spec.NodeName, "reason", reason)

		if vm.Spec.Lifecycle == impdevv1alpha1.VMLifecycleEphemeral {
			// Clear assignment — spec patch first.
			specPatch := client.MergeFrom(vm.DeepCopy())
			vm.Spec.NodeName = ""
			if err2 := r.Patch(ctx, vm, specPatch); err2 != nil {
				return ctrl.Result{}, err2
			}
			// Status patch — take vmCopy after spec patch so resourceVersion is current.
			vmCopy := vm.DeepCopy()
			setNodeUnhealthy(vm, reason)
			vm.Status.Phase = impdevv1alpha1.VMPhasePending
			setUnscheduled(vm)
			if err2 := r.Status().Patch(ctx, vm, client.MergeFrom(vmCopy)); err2 != nil {
				return ctrl.Result{}, err2
			}
			r.Recorder.Event(vm, corev1.EventTypeNormal, EventReasonRescheduling,
				"Ephemeral VM rescheduled after node loss")
			return ctrl.Result{}, nil
		}
		// Persistent → fail.
		vmCopy := vm.DeepCopy()
		setNodeUnhealthy(vm, reason)
		vm.Status.Phase = impdevv1alpha1.VMPhaseFailed
		if err2 := r.Status().Patch(ctx, vm, client.MergeFrom(vmCopy)); err2 != nil {
			return ctrl.Result{}, err2
		}
		r.Recorder.Event(vm, corev1.EventTypeWarning, EventReasonNodeLost,
			"Assigned node lost; persistent VM marked Failed")
		return ctrl.Result{}, nil
	}

	// Node healthy — take base before ALL mutations so diffs are non-empty.
	base := vm.DeepCopy()
	setNodeHealthy(vm)
	setScheduled(vm, vm.Spec.NodeName)

	var annotationChanged bool
	if vm.Status.Phase == impdevv1alpha1.VMPhaseRunning {
		httpSpec := resolveHTTPCheck(vm, r.globalHTTPCheck(ctx))
		if httpSpec != nil {
			annotationChanged = r.applyHTTPCheck(ctx, vm, httpSpec)
		} else {
			setReadyFromPhase(vm)
		}
	} else {
		setReadyFromPhase(vm)
	}

	// Status patch first (status subresource; base.rv == vm.rv so no rv in body → no conflict).
	if err2 := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err2 != nil {
		return ctrl.Result{}, err2
	}
	// Annotation patch second — vm.rv now updated by the status patch response,
	// so diff(base, vm) includes the new rv which satisfies the server's optimistic check.
	if annotationChanged {
		if err2 := r.Patch(ctx, vm, client.MergeFrom(base)); err2 != nil {
			return ctrl.Result{}, err2
		}
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
