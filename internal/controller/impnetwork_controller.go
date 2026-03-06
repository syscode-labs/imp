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

// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch

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

	// Emit CNIDetected (or CNIAmbiguous) event to confirm which CNI/NAT is in use for this network.
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

	// GC stale VTEP entries (VMs that are no longer Running on this network).
	if err := r.reconcileVTEPTable(ctx, net); err != nil {
		return ctrl.Result{}, err
	}

	// Update status: set Ready condition.
	base := net.DeepCopy()
	setNetworkReady(net)
	if err := r.Status().Patch(ctx, net, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// reconcileVTEPTable removes stale entries from ImpNetwork.status.vtepTable.
// An entry is stale if no Running ImpVM with that IP exists in this namespace
// referencing this network. The agent is responsible for adding entries.
func (r *ImpNetworkReconciler) reconcileVTEPTable(ctx context.Context, net *impdevv1alpha1.ImpNetwork) error {
	if len(net.Status.VTEPTable) == 0 {
		return nil
	}

	// List all ImpVMs in the same namespace that reference this network.
	var vmList impdevv1alpha1.ImpVMList
	if err := r.List(ctx, &vmList, client.InNamespace(net.Namespace)); err != nil {
		return err
	}

	// Build a set of IPs for Running VMs that reference this network.
	activeIPs := make(map[string]struct{})
	for i := range vmList.Items {
		vm := &vmList.Items[i]
		if vm.Spec.NetworkRef == nil || vm.Spec.NetworkRef.Name != net.Name {
			continue
		}
		if vm.Status.Phase == impdevv1alpha1.VMPhaseRunning && vm.Status.IP != "" {
			activeIPs[vm.Status.IP] = struct{}{}
		}
	}

	// Filter vtepTable to only active entries.
	filtered := net.Status.VTEPTable[:0]
	for _, entry := range net.Status.VTEPTable {
		if _, ok := activeIPs[entry.VMIP]; ok {
			filtered = append(filtered, entry)
		}
	}

	if len(filtered) == len(net.Status.VTEPTable) {
		return nil // no change
	}

	logf.FromContext(ctx).Info("GCing stale VTEP entries",
		"network", net.Name, "before", len(net.Status.VTEPTable), "after", len(filtered))

	base := net.DeepCopy()
	net.Status.VTEPTable = filtered
	return r.Status().Patch(ctx, net, client.MergeFrom(base))
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
