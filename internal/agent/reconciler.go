//go:build linux

package agent

import (
	"context"
	"errors"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/network"
)

// ImpVMReconciler watches ImpVM objects and drives VM lifecycle on this node.
// It filters to objects where spec.nodeName == NodeName — all others are ignored.
type ImpVMReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NodeName string
	// NodeIP is the node's InternalIP used for VTEP registration and VXLAN setup.
	// Sourced from NODE_IP env var (downward API fieldRef status.hostIP).
	NodeIP  string
	Driver  VMDriver
	Metrics *VMMetricsCollector
	// Net is optional. When non-nil, used for VXLAN/FDB operations after VTEP sync.
	Net network.NetManager
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks/status,verbs=get;update;patch

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

	if r.Metrics != nil {
		r.Metrics.SetVMState(vm.Namespace+"/"+vm.Name, "Running", r.NodeName)
	}

	// Register VTEP entry so the operator and other nodes know where this VM lives.
	if vm.Spec.NetworkRef != nil && state.IP != "" && r.NodeIP != "" {
		macAddr := network.MACAddr(vm.Namespace + "/" + vm.Name)
		if err := r.registerVTEP(ctx, vm, state.IP, macAddr); err != nil {
			log.Error(err, "registerVTEP failed — FDB sync may be incomplete")
		} else {
			// Sync local FDB now that this node has a VTEP entry.
			if err := r.syncFDB(ctx, vm); err != nil {
				log.Error(err, "syncFDB after registerVTEP failed")
			}
		}
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

	if r.Metrics != nil {
		r.Metrics.ClearVM(vm.Namespace + "/" + vm.Name)
	}

	// Deregister VTEP entry so other nodes stop routing to this (now stopped) VM.
	if vm.Spec.NetworkRef != nil && vm.Status.IP != "" {
		if err := r.deregisterVTEP(ctx, vm); err != nil {
			log.Error(err, "deregisterVTEP failed — stale entry will be GC'd by operator")
		}
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

// metricsServer is a controller-runtime Runnable that serves Prometheus metrics.
// Registered with the manager so it shuts down cleanly when the manager stops.
type metricsServer struct{ handler http.Handler }

func (s *metricsServer) Start(ctx context.Context) error {
	srv := &http.Server{Addr: metricsPort, Handler: s.handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background()) //nolint:errcheck
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// SetupWithManager registers the reconciler with the controller-runtime manager.
func (r *ImpVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Metrics != nil {
		mux := http.NewServeMux()
		mux.Handle("/metrics", NewMetricsHandlerWithCollector(r.Metrics))
		if err := mgr.Add(&metricsServer{handler: mux}); err != nil {
			return err
		}
	}

	// Detect and patch CPU model onto ClusterImpNodeProfile at startup (best-effort).
	go detectAndPatchCPUModel(context.Background(), r.Client, r.NodeName)

	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpVM{}).
		Named("agent-impvm").
		Complete(r)
}

// registerVTEP adds or updates the VTEPEntry for vm in ImpNetwork.status.vtepTable.
func (r *ImpVMReconciler) registerVTEP(ctx context.Context, vm *impdevv1alpha1.ImpVM, vmIP, vmMAC string) error {
	var impNet impdevv1alpha1.ImpNetwork
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: vm.Namespace,
		Name:      vm.Spec.NetworkRef.Name,
	}, &impNet); err != nil {
		return err
	}

	// Check if an up-to-date entry already exists.
	for _, e := range impNet.Status.VTEPTable {
		if e.VMIP == vmIP && e.VMMAC == vmMAC && e.NodeIP == r.NodeIP {
			return nil // already registered
		}
	}

	base := impNet.DeepCopy()

	// Replace or append entry for this VM IP.
	found := false
	for i, e := range impNet.Status.VTEPTable {
		if e.VMIP == vmIP {
			impNet.Status.VTEPTable[i] = impdevv1alpha1.VTEPEntry{
				NodeIP: r.NodeIP,
				VMIP:   vmIP,
				VMMAC:  vmMAC,
			}
			found = true
			break
		}
	}
	if !found {
		impNet.Status.VTEPTable = append(impNet.Status.VTEPTable, impdevv1alpha1.VTEPEntry{
			NodeIP: r.NodeIP,
			VMIP:   vmIP,
			VMMAC:  vmMAC,
		})
	}

	return r.Status().Patch(ctx, &impNet, client.MergeFrom(base))
}

// deregisterVTEP removes the VTEPEntry for vm.Status.IP from ImpNetwork.status.vtepTable.
func (r *ImpVMReconciler) deregisterVTEP(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	var impNet impdevv1alpha1.ImpNetwork
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: vm.Namespace,
		Name:      vm.Spec.NetworkRef.Name,
	}, &impNet); err != nil {
		return client.IgnoreNotFound(err)
	}

	// Filter out the entry for this VM.
	base := impNet.DeepCopy()
	filtered := make([]impdevv1alpha1.VTEPEntry, 0, len(impNet.Status.VTEPTable))
	for _, e := range impNet.Status.VTEPTable {
		if e.VMIP != vm.Status.IP {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == len(impNet.Status.VTEPTable) {
		return nil // nothing to remove
	}

	impNet.Status.VTEPTable = filtered
	return r.Status().Patch(ctx, &impNet, client.MergeFrom(base))
}

// syncFDB fetches the ImpNetwork for vm and reconciles the local VXLAN FDB.
// Only remote entries (not on this node) are passed to SyncFDB.
func (r *ImpVMReconciler) syncFDB(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	if r.Net == nil || r.NodeIP == "" {
		return nil
	}

	var impNet impdevv1alpha1.ImpNetwork
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: vm.Namespace,
		Name:      vm.Spec.NetworkRef.Name,
	}, &impNet); err != nil {
		return client.IgnoreNotFound(err)
	}

	vni, ifaceName := network.VXLANParams(string(impNet.UID))

	if err := r.Net.EnsureVXLAN(ctx, vni, ifaceName, r.NodeIP); err != nil {
		return err
	}

	// Collect remote entries (not on this node).
	var remoteEntries []network.FDBEntry
	for _, e := range impNet.Status.VTEPTable {
		if e.NodeIP == r.NodeIP {
			continue // skip local entries
		}
		remoteEntries = append(remoteEntries, network.FDBEntry{
			MAC:   e.VMMAC,
			DstIP: e.NodeIP,
		})
	}

	return r.Net.SyncFDB(ctx, ifaceName, remoteEntries)
}
