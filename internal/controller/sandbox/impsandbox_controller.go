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

// Package sandbox contains controllers for the optional Imp Sandbox add-on.
// These controllers are registered exclusively by cmd/sandbox; the base
// operator binary never imports this package.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/syscode-labs/imp/api/sandbox/v1alpha1"
	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
)

const (
	// sandboxFinalizer gates teardown so dependent base resources are removed
	// deterministically before the sandbox object disappears.
	sandboxFinalizer = "sandbox.imp.dev/teardown"

	// Condition types reported on ImpSandbox status.
	ConditionReady           = "Ready"
	ConditionTenancyEnforced = "TenancyEnforced"

	// Event reasons emitted by the sandbox controller.
	EventReasonVMCreated       = "SandboxVMCreated"
	EventReasonNetworkFailed   = "NetworkAllocationFailed"
	EventReasonTenancyDenied   = "TenancyRequirementUnmet"
	EventReasonSubnetExhausted = "SubnetExhausted"
)

// generatedSubnetBase is the /16 from which dedicated sandbox networks are
// carved. Chosen outside common pod/service CIDRs documented by major
// distributions (kind 10.244/16, k3s 10.42/16, docker 172.17/16); override
// requires code change until a config field is justified.
const generatedSubnetBase = "10.214"

// generatedSubnetPrefixLen sizes each sandbox network to the minimum that
// fits gateway + one VM: a /30 has exactly two usable host addresses.
// If multi-VM sandboxes are ever introduced, this becomes configurable.
const generatedSubnetPrefixLen = 30

// ImpSandboxReconciler reconciles ImpSandbox objects into owned base-group
// ImpVM and ImpNetwork resources.
type ImpSandboxReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Recorder emits Kubernetes events for sandbox lifecycle milestones.
	Recorder record.EventRecorder //nolint:staticcheck

	// CNIStore exposes CNI detection results shared at manager startup.
	CNIStore *cnidetect.Store
}

// +kubebuilder:rbac:groups=sandbox.imp.dev,resources=impsandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.imp.dev,resources=impsandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.imp.dev,resources=impsandboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworks,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=imp.dev,resources=clusterimpconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update

// Reconcile drives an ImpSandbox toward its desired backing microVM.
func (r *ImpSandboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	sb := &sandboxv1alpha1.ImpSandbox{}
	if err := r.Get(ctx, req.NamespacedName, sb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !sb.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, sb)
	}

	if !controllerutil.ContainsFinalizer(sb, sandboxFinalizer) {
		base := sb.DeepCopy()
		controllerutil.AddFinalizer(sb, sandboxFinalizer)
		if err := r.Patch(ctx, sb, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	effective, err := r.resolveTenancy(ctx, sb)
	if err != nil {
		var denied *TenancyDeniedError
		if errors.As(err, &denied) {
			log.Info("Tenancy requirement unmet", "reason", denied.Reason)
			r.Recorder.Event(sb, corev1.EventTypeWarning, EventReasonTenancyDenied, denied.Reason)
			r.updateStatus(ctx, sb, func(s *sandboxv1alpha1.ImpSandboxStatus) {
				apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
					Type:               ConditionTenancyEnforced,
					Status:             metav1.ConditionFalse,
					Reason:             EventReasonTenancyDenied,
					Message:            denied.Reason,
					LastTransitionTime: metav1.Now(),
				})
				setReadyCondition(s, metav1.ConditionFalse, EventReasonTenancyDenied, denied.Reason)
			})
			return ctrl.Result{RequeueAfter: retryInterval}, nil
		}
		return ctrl.Result{}, err
	}

	networkName := r.ensureNetworkName(ctx, sb)
	if err := r.ensureNetwork(ctx, sb, networkName); err != nil {
		var exhausted *SubnetExhaustedError
		reason := EventReasonNetworkFailed
		if errors.As(err, &exhausted) {
			reason = EventReasonSubnetExhausted
			r.Recorder.Event(sb, corev1.EventTypeWarning, reason, exhausted.Reason)
		} else {
			r.Recorder.Event(sb, corev1.EventTypeWarning, reason, fmt.Sprintf("Could not ensure ImpNetwork: %v", err))
		}
		r.updateStatus(ctx, sb, func(s *sandboxv1alpha1.ImpSandboxStatus) {
			setReadyCondition(s, metav1.ConditionFalse, reason, fmt.Sprintf("Could not ensure ImpNetwork: %v", err))
		})
		return ctrl.Result{}, err
	}

	vmName := sb.Name
	if err := r.ensureVM(ctx, sb, networkName, vmName); err != nil {
		return ctrl.Result{}, err
	}

	vm := &impv1alpha1.ImpVM{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: sb.Namespace, Name: vmName}, vm); err != nil {
		return ctrl.Result{}, err
	}
	if vm.Status.Phase == "" && vm.Generation == 1 {
		r.Recorder.Event(sb, corev1.EventTypeNormal, EventReasonVMCreated,
			fmt.Sprintf("Created ImpVM %s/%s on network %s", sb.Namespace, vmName, networkName))
	}

	r.updateStatus(ctx, sb, func(s *sandboxv1alpha1.ImpSandboxStatus) {
		s.VMName = vmName
		s.NetworkName = networkName
		s.EffectiveTenancy = effective
		apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
			Type:               ConditionTenancyEnforced,
			Status:             metav1.ConditionTrue,
			Reason:             "TenancyResolved",
			Message:            "Tenancy tier " + string(effective) + " enforced",
			LastTransitionTime: metav1.Now(),
		})
		setReadyFromPhase(s, vm.Status.Phase)
	})

	log.Info("Reconciled sandbox", "vm", vmName, "network", networkName, "tenancy", effective)
	return ctrl.Result{}, nil
}

func (r *ImpSandboxReconciler) reconcileDelete(ctx context.Context, sb *sandboxv1alpha1.ImpSandbox) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Delete the backing VM first so teardown order is deterministic even if
	// owner-reference GC is delayed.
	vm := &impv1alpha1.ImpVM{}
	err := r.Get(ctx, types.NamespacedName{Namespace: sb.Namespace, Name: sb.Name}, vm)
	switch {
	case err == nil:
		if vm.DeletionTimestamp.IsZero() {
			if delErr := r.Delete(ctx, vm); delErr != nil && !apierrors.IsNotFound(delErr) {
				return ctrl.Result{}, delErr
			}
		}
		log.Info("Waiting for backing ImpVM to terminate before releasing sandbox")
		return ctrl.Result{RequeueAfter: retryInterval}, nil
	case apierrors.IsNotFound(err):
		// proceed
	default:
		return ctrl.Result{}, err
	}

	if controllerutil.ContainsFinalizer(sb, sandboxFinalizer) {
		base := sb.DeepCopy()
		controllerutil.RemoveFinalizer(sb, sandboxFinalizer)
		if err := r.Patch(ctx, sb, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// resolveTenancy applies the cluster floor and verifies hard-tier capability.
func (r *ImpSandboxReconciler) resolveTenancy(ctx context.Context, sb *sandboxv1alpha1.ImpSandbox) (sandboxv1alpha1.TenancyMode, error) {
	requested := sb.Spec.Tenancy
	if requested == "" {
		requested = sandboxv1alpha1.TenancyStandard
	}

	floor := sandboxv1alpha1.TenancyStandard
	cfg := &impv1alpha1.ClusterImpConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, cfg); err == nil && cfg.Spec.Sandbox != nil && cfg.Spec.Sandbox.FloorTenancy != "" {
		floor = sandboxv1alpha1.TenancyMode(cfg.Spec.Sandbox.FloorTenancy)
	} else if err != nil && !apierrors.IsNotFound(err) {
		return "", err
	}

	effective := requested
	if tenancyRank(floor) > tenancyRank(requested) {
		return "", &TenancyDeniedError{Reason: fmt.Sprintf(
			"spec.tenancy %q is below the cluster floor %q set in ClusterImpConfig.sandbox.floorTenancy",
			requested, floor)}
	}

	if effective == sandboxv1alpha1.TenancyHard && !r.ciliumPresent() {
		return "", &TenancyDeniedError{Reason: "hard tenancy requires Cilium; no Cilium CRDs detected (fail closed)"}
	}
	return effective, nil
}

func (r *ImpSandboxReconciler) ciliumPresent() bool {
	if r.CNIStore == nil {
		return false
	}
	result, ok := r.CNIStore.Result()
	if !ok {
		return false
	}
	return result.Provider == cnidetect.ProviderCilium || result.Provider == cnidetect.ProviderCiliumKubeProxyFree
}

func (r *ImpSandboxReconciler) ensureNetworkName(_ context.Context, sb *sandboxv1alpha1.ImpSandbox) string {
	if sb.Spec.NetworkRef != nil {
		return sb.Spec.NetworkRef.Name
	}
	return sb.Name + "-net"
}

// ensureNetwork creates a dedicated ImpNetwork when none was referenced.
func (r *ImpSandboxReconciler) ensureNetwork(ctx context.Context, sb *sandboxv1alpha1.ImpSandbox, name string) error {
	if sb.Spec.NetworkRef != nil {
		return nil // attached networks are managed elsewhere
	}

	existing := &impv1alpha1.ImpNetwork{}
	err := r.Get(ctx, client.ObjectKey{Namespace: sb.Namespace, Name: name}, existing)
	if err == nil {
		before := existing.DeepCopy()
		desired := r.baselineDenyCIDRs(ctx)
		if existing.Spec.Firewall == nil {
			existing.Spec.Firewall = &impv1alpha1.FirewallSpec{DenyCIDRs: desired}
		} else {
			existing.Spec.Firewall.DenyCIDRs = desired
		}
		return r.Patch(ctx, existing, client.MergeFrom(before))
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	subnet, err := r.allocateSubnet(ctx)
	if err != nil {
		return &SubnetExhaustedError{Reason: err.Error()}
	}

	net := &impv1alpha1.ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   sb.Namespace,
			Labels:      sandboxLabels(sb),
			Annotations: map[string]string{sandboxv1alpha1.SubnetIndexAnnotation: subnet.index},
		},
		Spec: impv1alpha1.ImpNetworkSpec{
			Subnet: subnet.cidr,
			NAT:    impv1alpha1.NATSpec{Enabled: true},
			Firewall: &impv1alpha1.FirewallSpec{
				DenyCIDRs: r.baselineDenyCIDRs(ctx),
			},
		},
	}
	if err := controllerutil.SetControllerReference(sb, net, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, net); err != nil {
		return err
	}
	return nil
}

// ensureVM creates or reconciles the backing ImpVM toward the sandbox spec.
func (r *ImpSandboxReconciler) ensureVM(ctx context.Context, sb *sandboxv1alpha1.ImpSandbox, networkName, vmName string) error {
	desired := func(vm *impv1alpha1.ImpVM) {
		vm.Labels = mergeLabels(vm.Labels, sandboxLabels(sb))
		vm.Spec = impv1alpha1.ImpVMSpec{
			TemplateRef:  sb.Spec.TemplateRef,
			ClassRef:     sb.Spec.ClassRef,
			Image:        sb.Spec.Image,
			NetworkRef:   &impv1alpha1.LocalObjectRef{Name: networkName},
			Lifecycle:    impv1alpha1.VMLifecyclePersistent,
			NodeSelector: sb.Spec.NodeSelector,
			ExpireAfter:  sb.Spec.ExpireAfter.DeepCopy(),
		}
	}

	existing := &impv1alpha1.ImpVM{}
	err := r.Get(ctx, client.ObjectKey{Namespace: sb.Namespace, Name: vmName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		vm := &impv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmName,
				Namespace: sb.Namespace,
				Labels:    sandboxLabels(sb),
			},
		}
		desired(vm)
		if err := controllerutil.SetControllerReference(sb, vm, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, vm)
	case err != nil:
		return err
	}

	before := existing.DeepCopy()
	desired(existing)
	if err := controllerutil.SetControllerReference(sb, existing, r.Scheme); err != nil {
		return err
	}
	return r.Patch(ctx, existing, client.MergeFrom(before))
}

// metadataCIDR is the cloud-metadata endpoint every sandbox must never reach,
// regardless of tenancy tier.
const metadataCIDR = "169.254.169.254/32"

// baselineDenyCIDRs composes the unconditional deny list for generated
// networks: the cloud-metadata endpoint, every node's pod CIDR (covers
// sandbox-to-pod and cross-node VM-to-VM pivots), and the kube-apiserver
// ClusterIP discovered from the default kubernetes Service.
//
// Known limitation: other ClusterIPs in the service range remain reachable
// unless the cluster publishes a ServiceCIDR object; closing that gap is
// tracked with Phase C egress work.
func (r *ImpSandboxReconciler) baselineDenyCIDRs(ctx context.Context) []string {
	deny := []string{metadataCIDR}
	seen := map[string]bool{metadataCIDR: true}

	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err == nil {
		for i := range nodes.Items {
			for _, cidr := range nodes.Items[i].Spec.PodCIDRs {
				if !seen[cidr] {
					seen[cidr] = true
					deny = append(deny, cidr)
				}
			}
		}
	} else {
		logf.FromContext(ctx).Error(err, "Could not list nodes for baseline deny discovery")
	}

	svc := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: "default", Name: "kubernetes"}, svc); err == nil && svc.Spec.ClusterIP != "" {
		clusterIP := svc.Spec.ClusterIP + "/32"
		if !seen[clusterIP] {
			deny = append(deny, clusterIP)
		}
	}
	return deny
}

func (r *ImpSandboxReconciler) updateStatus(ctx context.Context, sb *sandboxv1alpha1.ImpSandbox, mutate func(*sandboxv1alpha1.ImpSandboxStatus)) {
	base := sb.DeepCopy()
	mutate(&sb.Status)
	if err := r.Status().Patch(ctx, sb, client.MergeFrom(base)); err != nil {
		logf.FromContext(ctx).Error(err, "Could not patch ImpSandbox status")
	}
}

func sandboxLabels(sb *sandboxv1alpha1.ImpSandbox) map[string]string {
	return map[string]string{
		sandboxv1alpha1.SandboxOwnerLabel: sb.Name,
	}
}

func mergeLabels(dst map[string]string, src map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		out[k] = v
	}
	return out
}

// allocatedSubnet is the result of a subnet scan: the CIDR to use plus its
// stable index (recorded as an annotation on the generated network).
type allocatedSubnet struct {
	cidr  string
	index string
}

// allocateSubnet picks the lowest free /30 inside the sandbox base /16 by
// scanning every existing ImpNetwork cluster-wide. Any network whose address
// range overlaps the base block marks its covered indices as taken —
// regardless of prefix length — so foreign or legacy networks can never be
// double-allocated.
func (r *ImpSandboxReconciler) allocateSubnet(ctx context.Context) (allocatedSubnet, error) {
	list := &impv1alpha1.ImpNetworkList{}
	if err := r.List(ctx, list); err != nil {
		return allocatedSubnet{}, err
	}

	used := occupiedIndices(list.Items)
	for idx := 0; idx < totalGeneratedSubnets(); idx++ {
		if !used[idx] {
			return allocatedSubnet{
				cidr:  indexToCIDR(idx),
				index: strconv.Itoa(idx),
			}, nil
		}
	}
	return allocatedSubnet{}, fmt.Errorf(
		"all %d %s.0.0/16 /%d sandbox subnets are in use",
		totalGeneratedSubnets(), generatedSubnetBase, generatedSubnetPrefixLen)
}

// baseBlock is the parsed sandbox allocation range.
var baseBlock = netip.MustParsePrefix(generatedSubnetBase + ".0.0/16")

// totalGeneratedSubnets returns how many generatedSubnetPrefixLen-sized
// networks fit inside the base /16 (e.g. 2^(30-16) = 16384 for /30 slots).
func totalGeneratedSubnets() int {
	return 1 << (generatedSubnetPrefixLen - baseBlock.Bits())
}

// occupiedIndices marks every /30 slot intersected by an existing network's
// subnet that falls anywhere inside the base block.
func occupiedIndices(nets []impv1alpha1.ImpNetwork) map[int]bool {
	used := map[int]bool{}
	for i := range nets {
		prefix, err := netip.ParsePrefix(nets[i].Spec.Subnet)
		if err != nil || !prefix.Overlaps(baseBlock) {
			continue
		}
		first := ipv4Offset(prefix.Masked().Addr())
		last := first + (1 << (32 - prefix.Bits())) - 1
		for idx := first / 4; idx <= last/4 && idx < totalGeneratedSubnets(); idx++ {
			used[idx] = true
		}
	}
	return used
}

// indexToCIDR converts a /30 slot index into its CIDR inside the base block.
func indexToCIDR(idx int) string {
	offset := idx * 4
	return fmt.Sprintf("%s.%d.%d/%d",
		generatedSubnetBase, offset/256, offset%256, generatedSubnetPrefixLen)
}

// ipv4Offset maps an IPv4 address to its position inside the base /16.
func ipv4Offset(addr netip.Addr) int {
	b := addr.As4()
	return int(b[2])<<8 | int(b[3])
}

func tenancyRank(t sandboxv1alpha1.TenancyMode) int {
	if t == sandboxv1alpha1.TenancyHard {
		return 2
	}
	return 1
}

// TenancyDeniedError marks a resolvable policy refusal rather than a system error.
type TenancyDeniedError struct {
	Reason string
}

func (e *TenancyDeniedError) Error() string { return e.Reason }

// SubnetExhaustedError marks allocation failure: every slot in the sandbox
// base block is taken. It is retryable — deleting sandboxes frees indices.
type SubnetExhaustedError struct {
	Reason string
}

func (e *SubnetExhaustedError) Error() string { return e.Reason }

const (
	retryInterval = 15 * time.Second
)

func setReadyCondition(s *sandboxv1alpha1.ImpSandboxStatus, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func setReadyFromPhase(s *sandboxv1alpha1.ImpSandboxStatus, phase impv1alpha1.VMPhase) {
	switch phase {
	case impv1alpha1.VMPhaseRunning:
		setReadyCondition(s, metav1.ConditionTrue, "Running", "Sandbox VM is running")
	case impv1alpha1.VMPhaseFailed, impv1alpha1.VMPhaseRetryExhausted:
		setReadyCondition(s, metav1.ConditionFalse, "VMFailed", "Sandbox VM failed: "+string(phase))
	default:
		setReadyCondition(s, metav1.ConditionUnknown, "Waiting", "Waiting for sandbox VM to start, phase "+string(phase))
	}
}

// SetupWithManager registers the ImpSandbox controller and watches owned
// ImpVMs so sandbox status tracks VM lifecycle changes.
func (r *ImpSandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.ImpSandbox{}).
		Watches(
			&impv1alpha1.ImpVM{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &sandboxv1alpha1.ImpSandbox{}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("impsandbox").
		Complete(r)
}

var _ reconcile.Reconciler = (*ImpSandboxReconciler)(nil)
