package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMSnapshotReconciler reconciles ImpVMSnapshot objects.
type ImpVMSnapshotReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots/finalizers,verbs=update

func (r *ImpVMSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	snap := &impv1alpha1.ImpVMSnapshot{}
	if err := r.Get(ctx, req.NamespacedName, snap); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Set initial phase
	if snap.Status.Phase == "" {
		base := snap.DeepCopy()
		snap.Status.Phase = "Pending"
		if err := r.Status().Patch(ctx, snap, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("ImpVMSnapshot created, set to Pending", "name", snap.Name)
	}

	// TODO: trigger agent snapshot, handle cron scheduling, OCI push.

	return ctrl.Result{}, nil
}

func (r *ImpVMSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impv1alpha1.ImpVMSnapshot{}).
		Complete(r)
}
