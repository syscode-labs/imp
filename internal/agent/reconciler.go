package agent

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMReconciler watches ImpVM objects and drives VM lifecycle on this node.
// It filters to objects where spec.nodeName == NodeName — all others are ignored.
type ImpVMReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NodeName string
	Driver   VMDriver
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvms/status,verbs=get;update;patch

func (r *ImpVMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("node", r.NodeName)

	vm := &impdevv1alpha1.ImpVM{}
	if err := r.Get(ctx, req.NamespacedName, vm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Filter: only reconcile VMs assigned to this node.
	if vm.Spec.NodeName != r.NodeName {
		return ctrl.Result{}, nil
	}

	log = log.WithValues("vm", req.NamespacedName, "phase", vm.Status.Phase)

	switch vm.Status.Phase {
	case impdevv1alpha1.VMPhaseTerminating:
		return r.handleTerminating(ctx, vm)
	case impdevv1alpha1.VMPhaseScheduled:
		return r.handleScheduled(ctx, vm)
	case impdevv1alpha1.VMPhaseRunning:
		return r.handleRunning(ctx, vm)
	case impdevv1alpha1.VMPhaseStarting:
		log.Info("VM is Starting — requeuing")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	default:
		// Pending, Succeeded, Failed — not our concern.
		return ctrl.Result{}, nil
	}
}

func (r *ImpVMReconciler) handleScheduled(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Set phase=Starting before calling driver to make concurrent reconciles idempotent.
	base := vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseStarting
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	pid, err := r.Driver.Start(ctx, vm)
	if err != nil {
		log.Error(err, "Driver Start failed")
		return ctrl.Result{}, err
	}

	state, err := r.Driver.Inspect(ctx, vm)
	if err != nil {
		log.Error(err, "Driver Inspect after Start failed")
		return ctrl.Result{}, err
	}

	base = vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
	vm.Status.IP = state.IP
	vm.Status.RuntimePID = pid
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("VM started", "pid", pid, "ip", state.IP)
	return ctrl.Result{}, nil
}

func (r *ImpVMReconciler) handleRunning(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	state, err := r.Driver.Inspect(ctx, vm)
	if err != nil {
		log.Error(err, "Driver Inspect failed")
		return ctrl.Result{}, err
	}

	if state.Running {
		return ctrl.Result{}, nil // watch-driven steady state
	}

	log.Info("VM process exited", "lifecycle", vm.Spec.Lifecycle)
	if vm.Spec.Lifecycle == impdevv1alpha1.VMLifecycleEphemeral {
		return r.finishSucceeded(ctx, vm)
	}
	return r.finishFailed(ctx, vm)
}

func (r *ImpVMReconciler) handleTerminating(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if err := r.Driver.Stop(ctx, vm); err != nil {
		log.Error(err, "Driver Stop failed — will retry")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
	}
	return r.clearOwnership(ctx, vm)
}

// finishSucceeded clears spec.nodeName (triggers operator finalizer) + sets phase=Succeeded.
func (r *ImpVMReconciler) finishSucceeded(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	// Spec patch first — spec.nodeName is a spec field, not a status field.
	specBase := vm.DeepCopy()
	vm.Spec.NodeName = ""
	if err := r.Patch(ctx, vm, client.MergeFrom(specBase)); err != nil {
		return ctrl.Result{}, err
	}
	// Status patch — take base AFTER spec patch so resourceVersion is current.
	base := vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseSucceeded
	vm.Status.IP = ""
	vm.Status.RuntimePID = 0
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// finishFailed sets phase=Failed; keeps spec.nodeName (operator handles cleanup for persistent VMs).
func (r *ImpVMReconciler) finishFailed(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	base := vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseFailed
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// clearOwnership clears spec.nodeName + status ip/pid after Terminating stop.
func (r *ImpVMReconciler) clearOwnership(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	specBase := vm.DeepCopy()
	vm.Spec.NodeName = ""
	if err := r.Patch(ctx, vm, client.MergeFrom(specBase)); err != nil {
		return ctrl.Result{}, err
	}
	base := vm.DeepCopy()
	vm.Status.IP = ""
	vm.Status.RuntimePID = 0
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the reconciler with the controller-runtime manager.
func (r *ImpVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpVM{}).
		Named("agent-impvm").
		Complete(r)
}
