package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMMigrationReconciler reconciles ImpVMMigration objects.
type ImpVMMigrationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvmmigrations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impvmmigrations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvmmigrations/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

func (r *ImpVMMigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	mig := &impv1alpha1.ImpVMMigration{}
	if err := r.Get(ctx, req.NamespacedName, mig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	switch mig.Status.Phase {
	case "":
		return r.handleEmpty(ctx, mig)
	case "Pending":
		return r.handlePending(ctx, mig)
	case "Snapshotting":
		return r.handleSnapshotting(ctx, mig)
	case "Restoring":
		return r.handleRestoring(ctx, mig)
	case "Succeeded", "Failed":
		return ctrl.Result{}, nil
	default:
		log.Info("unknown migration phase, ignoring", "phase", mig.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// handleEmpty transitions Phase="" → "Pending" and requeues.
func (r *ImpVMMigrationReconciler) handleEmpty(ctx context.Context, mig *impv1alpha1.ImpVMMigration) (ctrl.Result, error) {
	base := mig.DeepCopy()
	mig.Status.Phase = "Pending"
	if err := r.Status().Patch(ctx, mig, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// handlePending validates the source VM, selects a target node, creates a child
// ImpVMSnapshot, then advances to Phase="Snapshotting".
func (r *ImpVMMigrationReconciler) handlePending(ctx context.Context, mig *impv1alpha1.ImpVMMigration) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Validate source VM exists.
	vm := &impv1alpha1.ImpVM{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: mig.Spec.SourceVMNamespace,
		Name:      mig.Spec.SourceVMName,
	}, vm)
	if apierrors.IsNotFound(err) {
		return r.failMigration(ctx, mig, "source VM not found")
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// Select target node.
	if mig.Status.SelectedNode == "" {
		if mig.Spec.TargetNode != "" {
			base := mig.DeepCopy()
			mig.Status.SelectedNode = mig.Spec.TargetNode
			if err := r.Status().Patch(ctx, mig, client.MergeFrom(base)); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			selectedNode, selErr := r.selectMigrationTarget(ctx, vm)
			if selErr != nil {
				return ctrl.Result{}, selErr
			}
			if selectedNode == "" {
				if vm.Spec.NodeName == "" {
					// Source VM not yet scheduled; wait for placement.
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				}
				return r.failMigration(ctx, mig, "no CPU-compatible node available (NoCPUCompatibleNode)")
			}
			base := mig.DeepCopy()
			mig.Status.SelectedNode = selectedNode
			if err := r.Status().Patch(ctx, mig, client.MergeFrom(base)); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Create child ImpVMSnapshot.
	snap := &impv1alpha1.ImpVMSnapshot{}
	snap.Namespace = mig.Namespace
	snap.Name = "mig-" + mig.Name
	snap.Spec = impv1alpha1.ImpVMSnapshotSpec{
		SourceVMName:      mig.Spec.SourceVMName,
		SourceVMNamespace: mig.Spec.SourceVMNamespace,
		Storage:           impv1alpha1.SnapshotStorageSpec{Type: "node-local"},
	}
	if err := ctrl.SetControllerReference(mig, snap, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, snap); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, err
	}

	base := mig.DeepCopy()
	mig.Status.Phase = "Snapshotting"
	mig.Status.SnapshotRef = snap.Name
	if err := r.Status().Patch(ctx, mig, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("ImpVMMigration snapshotting", "name", mig.Name, "snapshot", snap.Name, "targetNode", mig.Status.SelectedNode)
	return ctrl.Result{}, nil
}

// handleSnapshotting waits for the child snapshot to reach a terminal state.
// On success it creates the target VM and advances to "Restoring".
// On failure it sets Phase="Failed".
func (r *ImpVMMigrationReconciler) handleSnapshotting(ctx context.Context, mig *impv1alpha1.ImpVMMigration) (ctrl.Result, error) {
	if mig.Status.SnapshotRef == "" {
		return r.failMigration(ctx, mig, "internal error: Snapshotting phase without SnapshotRef")
	}

	snap := &impv1alpha1.ImpVMSnapshot{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: mig.Namespace,
		Name:      mig.Status.SnapshotRef,
	}, snap); err != nil {
		if apierrors.IsNotFound(err) {
			// Snapshot was deleted; requeue and wait.
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	if snap.Status.TerminatedAt == nil {
		// Snapshot still in progress.
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if snap.Status.Phase != "Succeeded" {
		return r.failMigration(ctx, mig, "snapshot failed: phase="+snap.Status.Phase)
	}

	// Determine the execution reference to pass to the target VM.
	var executionRef string
	if snap.Status.LastExecutionRef != nil {
		executionRef = snap.Status.LastExecutionRef.Name
	} else {
		executionRef = snap.Name // fallback: use the snapshot itself
	}

	return r.createTargetVM(ctx, mig, executionRef)
}

// createTargetVM copies the source VM spec to a new ImpVM on the selected node,
// wiring in the snapshot execution reference, then advances to "Restoring".
func (r *ImpVMMigrationReconciler) createTargetVM(ctx context.Context, mig *impv1alpha1.ImpVMMigration, executionRef string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	srcVM := &impv1alpha1.ImpVM{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: mig.Spec.SourceVMNamespace,
		Name:      mig.Spec.SourceVMName,
	}, srcVM); err != nil {
		if apierrors.IsNotFound(err) {
			return r.failMigration(ctx, mig, "source VM disappeared before target could be created")
		}
		return ctrl.Result{}, err
	}

	targetVM := &impv1alpha1.ImpVM{}
	targetVM.Namespace = srcVM.Namespace
	targetVM.Name = "mig-" + mig.Name + "-target"
	targetVM.Spec = *srcVM.Spec.DeepCopy()
	targetVM.Spec.NodeName = mig.Status.SelectedNode
	targetVM.Spec.SnapshotRef = executionRef

	if err := ctrl.SetControllerReference(mig, targetVM, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, targetVM); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, err
	}

	base := mig.DeepCopy()
	mig.Status.Phase = "Restoring"
	mig.Status.TargetVMName = targetVM.Name
	if err := r.Status().Patch(ctx, mig, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("ImpVMMigration restoring", "name", mig.Name, "targetVM", targetVM.Name)
	return ctrl.Result{}, nil
}

// handleRestoring waits for the target VM to reach Running, then deletes the source
// VM and marks the migration Succeeded.
func (r *ImpVMMigrationReconciler) handleRestoring(ctx context.Context, mig *impv1alpha1.ImpVMMigration) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	targetVM := &impv1alpha1.ImpVM{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: mig.Spec.SourceVMNamespace,
		Name:      mig.Status.TargetVMName,
	}, targetVM); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	if targetVM.Status.Phase != impv1alpha1.VMPhaseRunning {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Delete source VM.
	srcVM := &impv1alpha1.ImpVM{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: mig.Spec.SourceVMNamespace,
		Name:      mig.Spec.SourceVMName,
	}, srcVM); err == nil {
		if delErr := r.Delete(ctx, srcVM); delErr != nil && !apierrors.IsNotFound(delErr) {
			return ctrl.Result{}, delErr
		}
	} else if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	base := mig.DeepCopy()
	mig.Status.Phase = "Succeeded"
	mig.Status.CompletedAt = &now
	if err := r.Status().Patch(ctx, mig, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("ImpVMMigration succeeded", "name", mig.Name)
	return ctrl.Result{}, nil
}

// failMigration patches the migration to Phase="Failed" with the given message.
func (r *ImpVMMigrationReconciler) failMigration(ctx context.Context, mig *impv1alpha1.ImpVMMigration, msg string) (ctrl.Result, error) {
	now := metav1.Now()
	base := mig.DeepCopy()
	mig.Status.Phase = "Failed"
	mig.Status.Message = msg
	mig.Status.CompletedAt = &now
	if err := r.Status().Patch(ctx, mig, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// selectMigrationTarget finds a node with a CPU model compatible with the source VM's node.
// Returns "" when no compatible node is found.
func (r *ImpVMMigrationReconciler) selectMigrationTarget(ctx context.Context, vm *impv1alpha1.ImpVM) (string, error) {
	log := logf.FromContext(ctx)
	if vm.Spec.NodeName == "" {
		return "", nil
	}

	// Get source node CPU model
	sourceProfile := &impv1alpha1.ClusterImpNodeProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: vm.Spec.NodeName}, sourceProfile); err != nil {
		log.V(1).Info("no ClusterImpNodeProfile for source node", "node", vm.Spec.NodeName)
		return "", nil
	}
	sourceCPU := sourceProfile.Spec.CPUModel

	// Find all profiles, pick first with matching CPU model on a different node
	profiles := &impv1alpha1.ClusterImpNodeProfileList{}
	if err := r.List(ctx, profiles); err != nil {
		return "", err
	}
	for i := range profiles.Items {
		p := &profiles.Items[i]
		if p.Name == vm.Spec.NodeName {
			continue // skip source
		}
		if sourceCPU == "" || p.Spec.CPUModel == sourceCPU {
			log.V(1).Info("selected migration target", "node", p.Name, "cpuModel", p.Spec.CPUModel)
			return p.Name, nil
		}
	}
	return "", nil
}

// nodeDrainMapper creates ImpVMMigration resources for all VMs on a node
// when it is tainted as unschedulable (node drain).
func (r *ImpVMMigrationReconciler) nodeDrainMapper(ctx context.Context, obj client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)
	k8sNode := &corev1.Node{}
	if err := r.Get(ctx, client.ObjectKey{Name: obj.GetName()}, k8sNode); err != nil {
		return nil
	}
	// Only act on nodes with the unschedulable taint
	draining := false
	for _, t := range k8sNode.Spec.Taints {
		if t.Key == "node.kubernetes.io/unschedulable" {
			draining = true
			break
		}
	}
	if !draining {
		return nil
	}

	vmList := &impv1alpha1.ImpVMList{}
	if err := r.List(ctx, vmList); err != nil {
		log.Error(err, "failed to list ImpVMs for drain migration")
		return nil
	}
	for i := range vmList.Items {
		vm := &vmList.Items[i]
		if vm.Spec.NodeName != obj.GetName() {
			continue
		}
		mig := &impv1alpha1.ImpVMMigration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "drain-" + vm.Name,
				Namespace: vm.Namespace,
			},
			Spec: impv1alpha1.ImpVMMigrationSpec{
				SourceVMName:      vm.Name,
				SourceVMNamespace: vm.Namespace,
			},
		}
		if err := r.Create(ctx, mig); err != nil && !apierrors.IsAlreadyExists(err) {
			log.Error(err, "failed to create drain migration", "vm", vm.Name)
		}
	}
	return nil // migrations trigger their own reconcile via Create events
}

func (r *ImpVMMigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impv1alpha1.ImpVMMigration{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeDrainMapper)).
		Complete(r)
}
