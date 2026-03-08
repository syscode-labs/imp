//go:build linux

package agent

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/network"
	"github.com/syscode-labs/imp/internal/tracing"
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
	// Alloc is the in-memory IP allocator. When non-nil, Reserve is called during
	// lazy reattach to restore IP state after agent restart.
	Alloc *network.Allocator
	// StartTimeout is how long a VM may remain in Starting before being
	// transitioned to Failed. Defaults to 5 minutes when zero.
	StartTimeout time.Duration
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks/status,verbs=get;update;patch

func (r *ImpVMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := logf.FromContext(ctx).WithValues("node", r.NodeName)

	vm := &impdevv1alpha1.ImpVM{}
	if err = r.Get(ctx, req.NamespacedName, vm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Filter: only reconcile VMs assigned to this node.
	if vm.Spec.NodeName != r.NodeName {
		return ctrl.Result{}, nil
	}

	ctx, span := otel.Tracer("imp.agent").Start(ctx, "agent.impvm.reconcile",
		trace.WithAttributes(
			attribute.String("vm.name", req.Name),
			attribute.String("vm.namespace", req.Namespace),
			attribute.String("vm.node", r.NodeName),
			attribute.String("vm.phase", string(vm.Status.Phase)),
		),
	)
	defer func() {
		tracing.RecordError(span, err)
		span.End()
	}()

	log = log.WithValues("vm", req.NamespacedName, "phase", vm.Status.Phase)

	switch vm.Status.Phase {
	case impdevv1alpha1.VMPhaseTerminating:
		return r.handleTerminating(ctx, vm)
	case impdevv1alpha1.VMPhaseScheduled:
		return r.handleScheduled(ctx, vm)
	case impdevv1alpha1.VMPhaseRunning:
		return r.handleRunning(ctx, vm)
	case impdevv1alpha1.VMPhaseStarting:
		return r.handleStarting(ctx, vm)
	default:
		// Pending, Succeeded, Failed — not our concern.
		return ctrl.Result{}, nil
	}
}

func (r *ImpVMReconciler) startTimeout() time.Duration {
	if r.StartTimeout > 0 {
		return r.StartTimeout
	}
	return 5 * time.Minute
}

func (r *ImpVMReconciler) handleStarting(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if vm.Status.StartedAt != nil {
		if elapsed := time.Since(vm.Status.StartedAt.Time); elapsed > r.startTimeout() {
			log.Info("VM stuck in Starting — timing out", "elapsed", elapsed, "timeout", r.startTimeout())
			return r.finishFailed(ctx, vm)
		}
	}
	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

func (r *ImpVMReconciler) handleScheduled(ctx context.Context, vm *impdevv1alpha1.ImpVM) (result ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	ctx, span := tracing.SpanFromVM(ctx, vm, "agent.impvm.start",
		trace.WithAttributes(
			attribute.String("vm.name", vm.Name),
			attribute.String("vm.namespace", vm.Namespace),
			attribute.String("vm.node", r.NodeName),
		),
	)
	defer func() {
		tracing.RecordError(span, err)
		span.End()
	}()

	// Set phase=Starting before calling driver to make concurrent reconciles idempotent.
	base := vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseStarting
	now := metav1.Now()
	vm.Status.StartedAt = &now
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
	vm.Status.StartedAt = nil
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
		{
			vCtx, vSpan := otel.Tracer("imp.agent").Start(ctx, "agent.impvm.vtep_register",
				trace.WithAttributes(
					attribute.String("vm.name", vm.Name),
					attribute.String("vm.ip", state.IP),
				),
			)
			vtepErr := r.registerVTEP(vCtx, vm, state.IP, macAddr)
			tracing.RecordError(vSpan, vtepErr)
			vSpan.End()
			if vtepErr != nil {
				log.Error(vtepErr, "registerVTEP failed — FDB sync may be incomplete")
			} else {
				// Sync local FDB now that this node has a VTEP entry.
				if err := r.syncFDB(ctx, vm); err != nil {
					log.Error(err, "syncFDB after registerVTEP failed")
				}
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

	// Inspect returned Running=false. Before declaring the VM dead, check whether
	// the Firecracker process is still alive (procs map may be empty after an
	// agent pod restart). If PID is alive, reattach and restore allocator state.
	if pid := vm.Status.RuntimePID; pid > 0 && r.Driver.IsAlive(pid) {
		rCtx, rSpan := otel.Tracer("imp.agent").Start(ctx, "agent.impvm.reattach",
			trace.WithAttributes(
				attribute.String("vm.name", vm.Name),
				attribute.String("vm.namespace", vm.Namespace),
				attribute.String("vm.pid", strconv.FormatInt(pid, 10)),
			),
		)
		reattachErr := r.Driver.Reattach(rCtx, vm)
		if reattachErr != nil {
			tracing.RecordError(rSpan, reattachErr)
			rSpan.End()
			log.Error(reattachErr, "Reattach failed — treating VM as dead")
		} else {
			// Restore in-memory IP allocation so Release works correctly later.
			if r.Alloc != nil && vm.Spec.NetworkRef != nil && vm.Status.IP != "" {
				netKey := vm.Namespace + "/" + vm.Spec.NetworkRef.Name
				r.Alloc.Reserve(netKey, vm.Status.IP)
			}
			// Re-publish VTEP entry and sync FDB in case they were lost.
			if vm.Spec.NetworkRef != nil && vm.Status.IP != "" && r.NodeIP != "" {
				macAddr := network.MACAddr(vm.Namespace + "/" + vm.Name)
				{
					vCtx, vSpan := otel.Tracer("imp.agent").Start(rCtx, "agent.impvm.vtep_register",
						trace.WithAttributes(
							attribute.String("vm.name", vm.Name),
							attribute.String("vm.ip", vm.Status.IP),
						),
					)
					vtepErr := r.registerVTEP(vCtx, vm, vm.Status.IP, macAddr)
					tracing.RecordError(vSpan, vtepErr)
					vSpan.End()
					if vtepErr != nil {
						log.Error(vtepErr, "registerVTEP after reattach failed")
					} else {
						fCtx, fSpan := otel.Tracer("imp.agent").Start(rCtx, "agent.impvm.fdb_sync",
							trace.WithAttributes(
								attribute.String("vm.name", vm.Name),
								attribute.String("net.name", vm.Spec.NetworkRef.Name),
							),
						)
						fdbErr := r.syncFDB(fCtx, vm)
						tracing.RecordError(fSpan, fdbErr)
						fSpan.End()
						if fdbErr != nil {
							log.Error(fdbErr, "syncFDB after reattach failed")
						}
					}
				}
			}
			rSpan.End()
			log.Info("VM reattached after agent restart", "pid", pid)
			return ctrl.Result{}, nil
		}
	}

	log.Info("VM process exited", "lifecycle", vm.Spec.Lifecycle)
	if vm.Spec.Lifecycle == impdevv1alpha1.VMLifecycleEphemeral {
		return r.finishSucceeded(ctx, vm)
	}
	return r.finishFailed(ctx, vm)
}

func (r *ImpVMReconciler) handleTerminating(ctx context.Context, vm *impdevv1alpha1.ImpVM) (result ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	ctx, span := otel.Tracer("imp.agent").Start(ctx, "agent.impvm.stop",
		trace.WithAttributes(
			attribute.String("vm.name", vm.Name),
			attribute.String("vm.namespace", vm.Namespace),
		),
	)
	defer func() {
		tracing.RecordError(span, err)
		span.End()
	}()

	if err = r.Driver.Stop(ctx, vm); err != nil {
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
	vm.Status.StartedAt = nil // clear so a future restart doesn't immediately re-timeout
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
// It uses optimistic-lock retries to handle concurrent patches from multiple agents.
func (r *ImpVMReconciler) registerVTEP(ctx context.Context, vm *impdevv1alpha1.ImpVM, vmIP, vmMAC string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
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

		return r.Status().Patch(ctx, &impNet,
			client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	})
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
	return syncNetworkFDB(ctx, &impNet, r.NodeIP, r.Net)
}
