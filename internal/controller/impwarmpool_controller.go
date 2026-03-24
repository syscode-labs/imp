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

// ImpWarmPoolReconciler reconciles ImpWarmPool objects.
type ImpWarmPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=imp.dev,resources=impwarmpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impwarmpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impwarmpools/finalizers,verbs=update
// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=impvmtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;create

func (r *ImpWarmPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	pool := &impv1alpha1.ImpWarmPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the ImpVMSnapshot named by spec.snapshotRef.
	snap := &impv1alpha1.ImpVMSnapshot{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: pool.Namespace,
		Name:      pool.Spec.SnapshotRef,
	}, snap); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ImpVMSnapshot not found, requeueing", "snapshotRef", pool.Spec.SnapshotRef)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// Pool stays idle until a base snapshot has been elected.
	baseSnapshot := snap.Status.BaseSnapshot
	if baseSnapshot == "" {
		log.Info("No base snapshot elected yet, pool idle", "pool", pool.Name, "snapshotRef", pool.Spec.SnapshotRef)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Fetch the ImpVMTemplate to build VM specs.
	tpl := &impv1alpha1.ImpVMTemplate{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: pool.Namespace,
		Name:      pool.Spec.TemplateName,
	}, tpl); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ImpVMTemplate not found, requeueing", "templateName", pool.Spec.TemplateName)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// List pool members: ImpVMs labeled with this pool name.
	vmList := &impv1alpha1.ImpVMList{}
	if err := r.List(ctx, vmList,
		client.InNamespace(pool.Namespace),
		client.MatchingLabels{impv1alpha1.LabelWarmPool: pool.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Count ready (Running) and active (not Failed/RetryExhausted) members.
	var readyCount, activeCount int32
	for i := range vmList.Items {
		phase := vmList.Items[i].Status.Phase
		switch phase {
		case impv1alpha1.VMPhaseRunning:
			readyCount++
			activeCount++
		case impv1alpha1.VMPhaseFailed, impv1alpha1.VMPhaseRetryExhausted, impv1alpha1.VMPhaseSucceeded:
			// terminal — do not count as active
		default:
			// Pending, Scheduled, Starting, Terminating — count as active
			activeCount++
		}
	}

	// Create missing VMs to reach spec.size.
	toCreate := pool.Spec.Size - activeCount
	if toCreate < 0 {
		toCreate = 0
	}
	for i := int32(0); i < toCreate; i++ {
		if err := r.createPoolMember(ctx, pool, tpl, baseSnapshot); err != nil {
			return ctrl.Result{}, err
		}
	}
	if toCreate > 0 {
		log.Info("Created pool members", "pool", pool.Name, "count", toCreate, "baseSnapshot", baseSnapshot)
	}

	// Patch status.readyCount.
	base := pool.DeepCopy()
	pool.Status.ReadyCount = readyCount
	if err := r.Status().Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// createPoolMember creates a single ImpVM pool member.
func (r *ImpWarmPoolReconciler) createPoolMember(
	ctx context.Context,
	pool *impv1alpha1.ImpWarmPool,
	tpl *impv1alpha1.ImpVMTemplate,
	baseSnapshot string,
) error {
	vm := &impv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-",
			Namespace:    pool.Namespace,
			Labels: map[string]string{
				impv1alpha1.LabelWarmPool: pool.Name,
			},
		},
		Spec: impv1alpha1.ImpVMSpec{
			ClassRef:    &tpl.Spec.ClassRef,
			Image:       tpl.Spec.Image,
			SnapshotRef: baseSnapshot,
		},
	}
	if tpl.Spec.NetworkRef != nil {
		vm.Spec.NetworkRef = tpl.Spec.NetworkRef
	}
	if tpl.Spec.RestartPolicy != nil {
		vm.Spec.RestartPolicy = tpl.Spec.RestartPolicy
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
	if pool.Spec.ExpireAfter != nil {
		d := *pool.Spec.ExpireAfter
		vm.Spec.ExpireAfter = &d
	} else if tpl.Spec.ExpireAfter != nil {
		d := *tpl.Spec.ExpireAfter
		vm.Spec.ExpireAfter = &d
	}

	if err := ctrl.SetControllerReference(pool, vm, r.Scheme); err != nil {
		return err
	}

	return r.Create(ctx, vm)
}

func (r *ImpWarmPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impv1alpha1.ImpWarmPool{}).
		Owns(&impv1alpha1.ImpVM{}).
		Complete(r)
}
