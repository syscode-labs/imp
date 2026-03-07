//go:build linux

package agent

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/network"
)

// ImpNetworkReconciler watches ImpNetwork objects on behalf of the node agent.
// When VTEPTable changes, it syncs the local VXLAN FDB for running local VMs.
type ImpNetworkReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NodeName string
	NodeIP   string
	Net      network.NetManager
}

// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks/status,verbs=get;update;patch

func (r *ImpNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var impNet impdevv1alpha1.ImpNetwork
	if err := r.Get(ctx, req.NamespacedName, &impNet); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Only sync FDB if this node has at least one running VM on this network.
	var vmList impdevv1alpha1.ImpVMList
	if err := r.List(ctx, &vmList, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	hasLocalVM := false
	for i := range vmList.Items {
		vm := &vmList.Items[i]
		if vm.Spec.NodeName == r.NodeName &&
			vm.Spec.NetworkRef != nil &&
			vm.Spec.NetworkRef.Name == impNet.Name &&
			vm.Status.Phase == impdevv1alpha1.VMPhaseRunning {
			hasLocalVM = true
			break
		}
	}
	if !hasLocalVM {
		return ctrl.Result{}, nil
	}

	if err := syncNetworkFDB(ctx, &impNet, r.NodeIP, r.Net); err != nil {
		logf.FromContext(ctx).WithValues("node", r.NodeName).Error(err, "syncNetworkFDB failed")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ImpNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpNetwork{}).
		Named("agent-impnetwork").
		Complete(r)
}

// syncNetworkFDB ensures VXLAN exists for the network and reconciles FDB with
// remote (non-local) VTEP entries.
func syncNetworkFDB(ctx context.Context, netObj *impdevv1alpha1.ImpNetwork, nodeIP string, mgr network.NetManager) error {
	if mgr == nil || nodeIP == "" {
		return nil
	}

	netKey := netObj.Namespace + "/" + netObj.Name
	bridgeName := network.BridgeName(netKey)
	vni, ifaceName := network.VXLANParams(string(netObj.UID))

	if err := mgr.EnsureVXLAN(ctx, vni, ifaceName, nodeIP, bridgeName); err != nil {
		return err
	}

	var remoteEntries []network.FDBEntry
	for _, e := range netObj.Status.VTEPTable {
		if e.NodeIP == nodeIP {
			continue
		}
		remoteEntries = append(remoteEntries, network.FDBEntry{
			MAC:   e.VMMAC,
			DstIP: e.NodeIP,
		})
	}
	return mgr.SyncFDB(ctx, ifaceName, remoteEntries)
}
