package controller

import (
	"context"

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

	if mig.Status.Phase != "" {
		return ctrl.Result{}, nil // already initialised
	}

	base := mig.DeepCopy()
	mig.Status.Phase = "Pending"

	// Validate source VM exists
	vm := &impv1alpha1.ImpVM{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: mig.Spec.SourceVMNamespace, Name: mig.Spec.SourceVMName,
	}, vm)
	if apierrors.IsNotFound(err) {
		mig.Status.Phase = "Failed"
		mig.Status.Message = "source VM not found"
	} else if err != nil {
		return ctrl.Result{}, err
	} else if mig.Spec.TargetNode != "" {
		mig.Status.SelectedNode = mig.Spec.TargetNode
	} else {
		// CPU-compatible node selection
		selectedNode, selErr := r.selectMigrationTarget(ctx, vm)
		if selErr != nil {
			return ctrl.Result{}, selErr
		}
		if selectedNode == "" {
			mig.Status.Phase = "Failed"
			mig.Status.Message = "no CPU-compatible node available (NoCPUCompatibleNode)"
		} else {
			mig.Status.SelectedNode = selectedNode
		}
	}

	if err := r.Status().Patch(ctx, mig, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("ImpVMMigration initialised", "name", mig.Name, "phase", mig.Status.Phase,
		"targetNode", mig.Status.SelectedNode)

	// TODO: Phase 2 impl: pause VM → snapshot → restore on target → delete source.

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
