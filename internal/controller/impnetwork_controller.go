package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
	"github.com/syscode-labs/imp/internal/tracing"
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
// +kubebuilder:rbac:groups=cilium.io,resources=ciliumexternalworkloads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cilium.io,resources=ciliumpodippools,verbs=get;list;watch;create;update;patch;delete

func (r *ImpNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	net := &impdevv1alpha1.ImpNetwork{}
	if err := r.Get(ctx, req.NamespacedName, net); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ctx, span := otel.Tracer("imp.operator").Start(ctx, "operator.impnetwork.reconcile",
		trace.WithAttributes(
			attribute.String("net.name", req.Name),
			attribute.String("net.namespace", req.Namespace),
		))
	defer func() { tracing.RecordError(span, err); span.End() }()

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

	// GC the CiliumPodIPPool if Cilium IPAM is configured.
	// CiliumPodIPPool is cluster-scoped so owner references are not possible;
	// we track it with labels instead and delete it explicitly here.
	if net.Spec.IPAM != nil && net.Spec.IPAM.Provider == "cilium" &&
		net.Spec.IPAM.Cilium != nil && net.Spec.IPAM.Cilium.PoolRef != "" {
		pool := &unstructured.Unstructured{}
		pool.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "cilium.io",
			Version: "v2alpha1",
			Kind:    "CiliumPodIPPool",
		})
		pool.SetName(net.Spec.IPAM.Cilium.PoolRef)
		if err := r.Delete(ctx, pool); err != nil && !apierrors.IsNotFound(err) && !apimeta.IsNoMatchError(err) {
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(net, finalizerImpNetwork)
	return ctrl.Result{}, r.Update(ctx, net)
}

// reconcileCiliumPool creates or updates the CiliumPodIPPool owned by this ImpNetwork.
// No-op when spec.ipam.provider is not "cilium" or poolRef is unset.
func (r *ImpNetworkReconciler) reconcileCiliumPool(ctx context.Context, net *impdevv1alpha1.ImpNetwork) error {
	if net.Spec.IPAM == nil || net.Spec.IPAM.Provider != "cilium" {
		return nil
	}
	if net.Spec.IPAM.Cilium == nil || net.Spec.IPAM.Cilium.PoolRef == "" {
		return nil
	}

	cidr := net.Spec.Subnet
	if net.Spec.IPAM.Cilium.Cidr != "" {
		cidr = net.Spec.IPAM.Cilium.Cidr
	}

	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cilium.io",
		Version: "v2alpha1",
		Kind:    "CiliumPodIPPool",
	})
	pool.SetName(net.Spec.IPAM.Cilium.PoolRef)

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, pool, func() error {
		// CiliumPodIPPool is cluster-scoped; owner references from a namespace-scoped
		// resource are not allowed. Use labels for tracking instead.
		labels := pool.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["imp.dev/network"] = net.Name
		labels["imp.dev/namespace"] = net.Namespace
		pool.SetLabels(labels)
		return unstructured.SetNestedSlice(pool.Object, []interface{}{
			map[string]interface{}{"cidr": cidr},
		}, "spec", "ipv4", "cidrs")
	})
	if err != nil {
		// If CiliumPodIPPool CRD is not installed, log and skip rather than failing.
		if apimeta.IsNoMatchError(err) {
			logf.FromContext(ctx).Info("CiliumPodIPPool CRD not found, skipping pool reconcile")
			return nil
		}
		return fmt.Errorf("reconcile CiliumPodIPPool %q: %w", net.Spec.IPAM.Cilium.PoolRef, err)
	}
	return nil
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

	// Auto-create/update CiliumPodIPPool when using Cilium IPAM.
	if err := r.reconcileCiliumPool(ctx, net); err != nil {
		return ctrl.Result{}, err
	}

	// GC stale VTEP entries (VMs that are no longer Running on this network).
	if err := r.reconcileVTEPTable(ctx, net); err != nil {
		return ctrl.Result{}, err
	}

	// Allocate and record group subnets.
	if err := r.reconcileGroupCIDRs(ctx, net); err != nil {
		return ctrl.Result{}, err
	}

	// Enroll Running VMs as Cilium external workloads (no-op if Cilium absent).
	if err := r.reconcileCiliumEnrollment(ctx, net); err != nil {
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
func (r *ImpNetworkReconciler) reconcileVTEPTable(ctx context.Context, net *impdevv1alpha1.ImpNetwork) (err error) {
	ctx, span := otel.Tracer("imp.operator").Start(ctx, "operator.impnetwork.vtep_gc",
		trace.WithAttributes(
			attribute.String("net.name", net.Name),
			attribute.String("net.namespace", net.Namespace),
		))
	defer func() { tracing.RecordError(span, err); span.End() }()

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
	base := net.DeepCopy()
	filtered := make([]impdevv1alpha1.VTEPEntry, 0, len(net.Status.VTEPTable))
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

// ciliumPresent returns true if CiliumExternalWorkload CRDs are registered in the cluster.
func (r *ImpNetworkReconciler) ciliumPresent() bool {
	_, err := r.Client.RESTMapper().ResourcesFor(schema.GroupVersionResource{
		Group:    impdevv1alpha1.CiliumGroup,
		Version:  impdevv1alpha1.CiliumVersion,
		Resource: impdevv1alpha1.CiliumEWResource,
	})
	return err == nil
}

// reconcileGroupCIDRs derives subnet CIDRs for each network group in spec.groups
// and stores the result in status.groupCIDRs. Idempotent: skips the status patch
// when the computed CIDRs match what is already recorded.
func (r *ImpNetworkReconciler) reconcileGroupCIDRs(ctx context.Context, net *impdevv1alpha1.ImpNetwork) error {
	desired, err := carveGroupCIDRs(net.Spec.Subnet, net.Spec.Groups)
	if err != nil {
		logf.FromContext(ctx).Error(err, "group CIDR carving failed", "network", net.Name)
		r.Recorder.Eventf(net, corev1.EventTypeWarning, EventReasonGroupCIDRError,
			"Group CIDR carving failed: %v", err)
		return nil // don't block reconcile for this — operator continues without group CIDRs
	}

	if reflect.DeepEqual(net.Status.GroupCIDRs, desired) {
		return nil
	}

	base := net.DeepCopy()
	net.Status.GroupCIDRs = desired
	return r.Status().Patch(ctx, net, client.MergeFrom(base))
}

// reconcileCiliumEnrollment creates CiliumExternalWorkload objects for Running VMs
// attached to this network, and GCs CEWs for VMs that are no longer Running.
// It is a no-op when Cilium is not the active CNI or its CRDs are not present.
func (r *ImpNetworkReconciler) reconcileCiliumEnrollment(ctx context.Context, net *impdevv1alpha1.ImpNetwork) (err error) {
	ctx, span := otel.Tracer("imp.operator").Start(ctx, "operator.impnetwork.cilium_enroll",
		trace.WithAttributes(
			attribute.String("net.name", net.Name),
			attribute.String("net.namespace", net.Namespace),
		))
	defer func() { tracing.RecordError(span, err); span.End() }()

	log := logf.FromContext(ctx)

	// Guard: only proceed when Cilium is the detected CNI and its CRDs exist.
	cniResult, _ := r.CNIStore.Result()
	isCiliumCNI := cniResult.Provider == cnidetect.ProviderCilium ||
		cniResult.Provider == cnidetect.ProviderCiliumKubeProxyFree
	if !isCiliumCNI || !r.ciliumPresent() {
		return nil
	}

	// List all ImpVMs in the network's namespace referencing this network.
	var vmList impdevv1alpha1.ImpVMList
	if err := r.List(ctx, &vmList, client.InNamespace(net.Namespace)); err != nil {
		return err
	}

	// Build set of Running VMs that reference this network (by name).
	runningVMs := make(map[string]*impdevv1alpha1.ImpVM)
	for i := range vmList.Items {
		vm := &vmList.Items[i]
		if vm.Spec.NetworkRef == nil || vm.Spec.NetworkRef.Name != net.Name {
			continue
		}
		if vm.Status.Phase == impdevv1alpha1.VMPhaseRunning {
			// Key by namespace/name so VMs with the same name in different namespaces
			// are distinct — CEWs are cluster-scoped and names must be globally unique.
			runningVMs[vm.Namespace+"/"+vm.Name] = vm
		}
	}

	// List existing CEWs labelled for this network.
	cewList := &unstructured.UnstructuredList{}
	cewList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   impdevv1alpha1.CiliumGroup,
		Version: impdevv1alpha1.CiliumVersion,
		Kind:    "CiliumExternalWorkloadList",
	})
	if err := r.List(ctx, cewList,
		client.MatchingLabels{
			"imp.dev/network":   net.Name,
			"imp.dev/namespace": net.Namespace,
		},
	); err != nil && !apimeta.IsNoMatchError(err) {
		return err
	}

	// Build existing-CEW set before GC so the create loop is accurate.
	existingCEWNames := make(map[string]struct{}, len(cewList.Items))
	for i := range cewList.Items {
		existingCEWNames[cewList.Items[i].GetName()] = struct{}{}
	}

	// GC: delete CEWs for VMs that are no longer Running.
	for i := range cewList.Items {
		cew := &cewList.Items[i]
		vmKey := cew.GetLabels()["imp.dev/vm-key"] // namespace/name
		if _, ok := runningVMs[vmKey]; !ok {
			log.Info("deleting stale CiliumExternalWorkload", "cew", cew.GetName(), "vmKey", vmKey)
			if err := r.Delete(ctx, cew); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			delete(existingCEWNames, cew.GetName())
		}
	}

	// Create CEWs for Running VMs that don't have one yet.
	for _, vm := range runningVMs {
		// Include namespace in the name to avoid collisions across namespaces
		// (CiliumExternalWorkload is cluster-scoped).
		cewName := "vm-" + vm.Namespace + "-" + vm.Name
		if _, exists := existingCEWNames[cewName]; exists {
			continue
		}

		cew := &unstructured.Unstructured{}
		cew.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   impdevv1alpha1.CiliumGroup,
			Version: impdevv1alpha1.CiliumVersion,
			Kind:    "CiliumExternalWorkload",
		})
		cew.SetName(cewName)
		cew.SetLabels(map[string]string{
			"imp.dev/vm-key":    vm.Namespace + "/" + vm.Name,
			"imp.dev/vm":        vm.Name,
			"imp.dev/namespace": vm.Namespace,
			"imp.dev/network":   net.Name,
		})
		if vm.Status.IP != "" {
			if err := unstructured.SetNestedField(cew.Object, vm.Status.IP+"/32", "spec", "ipv4AllocCIDR"); err != nil {
				log.Error(err, "failed to set ipv4AllocCIDR", "vm", vm.Name)
			}
		}

		log.Info("creating CiliumExternalWorkload", "cew", cewName, "vm", vm.Name)
		if err := r.Create(ctx, cew); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

// vmToNetworkMapper maps an ImpVM event to the ImpNetwork it references,
// so changes to VM phase/IP trigger a re-reconcile of the parent network.
func vmToNetworkMapper(_ context.Context, obj client.Object) []ctrl.Request {
	vm, ok := obj.(*impdevv1alpha1.ImpVM)
	if !ok || vm.Spec.NetworkRef == nil {
		return nil
	}
	return []ctrl.Request{
		{NamespacedName: types.NamespacedName{
			Name:      vm.Spec.NetworkRef.Name,
			Namespace: vm.Namespace,
		}},
	}
}

// SetupWithManager registers the reconciler with the manager.
func (r *ImpNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpNetwork{}).
		Watches(
			&impdevv1alpha1.ImpVM{},
			handler.EnqueueRequestsFromMapFunc(vmToNetworkMapper),
		).
		Named("impnetwork").
		Complete(r)
}
