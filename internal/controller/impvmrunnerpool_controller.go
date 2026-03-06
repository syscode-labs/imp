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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMRunnerPoolReconciler reconciles ImpVMRunnerPool objects.
type ImpVMRunnerPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvmrunnerpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impvmrunnerpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvmtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;create;delete

func (r *ImpVMRunnerPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	pool := &impv1alpha1.ImpVMRunnerPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch template.
	tpl := &impv1alpha1.ImpVMTemplate{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: pool.Namespace, Name: pool.Spec.TemplateName}, tpl); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ImpVMTemplate not found, requeueing", "template", pool.Spec.TemplateName)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// List pool members.
	vmList := &impv1alpha1.ImpVMList{}
	if err := r.List(ctx, vmList,
		client.InNamespace(pool.Namespace),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: pool.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Delete terminal VMs — runners are single-use.
	for i := range vmList.Items {
		vm := &vmList.Items[i]
		switch vm.Status.Phase {
		case impv1alpha1.VMPhaseSucceeded, impv1alpha1.VMPhaseFailed, impv1alpha1.VMPhaseRetryExhausted:
			if err := r.Delete(ctx, vm); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			log.Info("deleted terminal runner VM", "vm", vm.Name, "phase", vm.Status.Phase)
		}
	}

	// Re-list after deletion.
	if err := r.List(ctx, vmList,
		client.InNamespace(pool.Namespace),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: pool.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Count active (non-terminal) VMs.
	var activeCount int32
	for i := range vmList.Items {
		switch vmList.Items[i].Status.Phase {
		case impv1alpha1.VMPhaseSucceeded, impv1alpha1.VMPhaseFailed, impv1alpha1.VMPhaseRetryExhausted:
			// skip — just deleted
		default:
			activeCount++
		}
	}

	// Determine how many VMs to create.
	minIdle := int32(0)
	maxConcurrent := int32(10)
	if pool.Spec.Scaling != nil {
		minIdle = pool.Spec.Scaling.MinIdle
		maxConcurrent = pool.Spec.Scaling.MaxConcurrent
	}

	toCreate := minIdle - activeCount
	if toCreate < 0 {
		toCreate = 0
	}
	available := maxConcurrent - activeCount
	if available < 0 {
		available = 0
	}
	if toCreate > available {
		toCreate = available
	}

	for i := int32(0); i < toCreate; i++ {
		if err := r.createRunnerVM(ctx, pool, tpl); err != nil {
			return ctrl.Result{}, err
		}
	}
	if toCreate > 0 {
		log.Info("created runner VMs", "pool", pool.Name, "count", toCreate)
	}

	// Patch status.
	base := pool.DeepCopy()
	pool.Status.ActiveCount = activeCount
	pool.Status.IdleCount = activeCount // approximation; per-VM idle tracking is future work
	if err := r.Status().Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue.
	requeueAfter := 30 * time.Second
	if pool.Spec.JobDetection != nil &&
		pool.Spec.JobDetection.Polling != nil &&
		pool.Spec.JobDetection.Polling.Enabled &&
		pool.Spec.JobDetection.Polling.IntervalSeconds > 0 {
		requeueAfter = time.Duration(pool.Spec.JobDetection.Polling.IntervalSeconds) * time.Second
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *ImpVMRunnerPoolReconciler) createRunnerVM(ctx context.Context, pool *impv1alpha1.ImpVMRunnerPool, tpl *impv1alpha1.ImpVMTemplate) error {
	vm := &impv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-",
			Namespace:    pool.Namespace,
			Labels:       map[string]string{impv1alpha1.LabelRunnerPool: pool.Name},
		},
		Spec: impv1alpha1.ImpVMSpec{
			ClassRef:  &tpl.Spec.ClassRef,
			Image:     tpl.Spec.Image,
			Lifecycle: impv1alpha1.VMLifecycleEphemeral,
		},
	}
	if tpl.Spec.NetworkRef != nil {
		vm.Spec.NetworkRef = tpl.Spec.NetworkRef
	}
	if tpl.Spec.Probes != nil {
		vm.Spec.Probes = tpl.Spec.Probes
	}
	if tpl.Spec.GuestAgent != nil {
		vm.Spec.GuestAgent = tpl.Spec.GuestAgent
	}
	if tpl.Spec.NetworkGroup != "" {
		vm.Spec.NetworkGroup = tpl.Spec.NetworkGroup
	}
	if err := ctrl.SetControllerReference(pool, vm, r.Scheme); err != nil {
		return err
	}
	return r.Create(ctx, vm)
}

func (r *ImpVMRunnerPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impv1alpha1.ImpVMRunnerPool{}).
		Owns(&impv1alpha1.ImpVM{}).
		Complete(r)
}
