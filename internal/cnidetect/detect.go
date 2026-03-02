package cnidetect

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// Provider identifies the CNI plugin running in the cluster.
type Provider string

const (
	ProviderCiliumKubeProxyFree Provider = "cilium-kubeproxy-free"
	ProviderCilium              Provider = "cilium"
	ProviderFlannel             Provider = "flannel"
	ProviderCalico              Provider = "calico"
	ProviderUnknown             Provider = "unknown"
)

// NATBackend selects the kernel NAT implementation for ImpNetwork rules.
type NATBackend string

const (
	NATBackendNftables NATBackend = "nftables"
	NATBackendIPTables NATBackend = "iptables"
)

// Result is the output of a CNI detection run.
type Result struct {
	Provider   Provider
	NATBackend NATBackend
	Ambiguous  bool // true if multiple CNIs were detected simultaneously
}

// Detect inspects the cluster and returns the active CNI and appropriate NAT backend.
//
// Detection priority (design doc §9.1):
//  1. Explicit ClusterImpConfig.spec.networking.cni.provider — skips all detection.
//  2. CRD presence via REST mapper:
//     ciliumnetworkpolicies.cilium.io → Cilium
//     globalnetworkpolicies.projectcalico.org → Calico
//  3. DaemonSet presence in kube-system (403 errors handled gracefully):
//     cilium-agent, kube-flannel-ds, calico-node
//  4. Multiple signals → Ambiguous, iptables fallback.
//  5. No signals → Unknown, iptables fallback.
func Detect(ctx context.Context, c client.Client) (Result, error) {
	log := logf.FromContext(ctx).WithName("cnidetect")

	// 1. Explicit provider in ClusterImpConfig singleton.
	cfg := &impdevv1alpha1.ClusterImpConfig{}
	if err := c.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); err == nil {
		if p := cfg.Spec.Networking.CNI.Provider; p != "" {
			log.Info("using explicit CNI provider", "provider", p)
			return resultFromProvider(Provider(p)), nil
		}
	}

	// 2. CRD-based detection via REST mapper (no apiextensions scheme needed).
	var signals []Provider
	if hasCRD(c, "cilium.io", "ciliumnetworkpolicies") {
		signals = append(signals, ProviderCilium)
	}
	if hasCRD(c, "projectcalico.org", "globalnetworkpolicies") {
		signals = append(signals, ProviderCalico)
	}

	// 3. DaemonSet-based detection (graceful error handling).
	if !containsProvider(signals, ProviderCilium) {
		if hasDaemonSet(ctx, c, "cilium-agent") {
			signals = append(signals, ProviderCilium)
		}
	}
	if !containsProvider(signals, ProviderFlannel) {
		if hasDaemonSet(ctx, c, "kube-flannel-ds") {
			signals = append(signals, ProviderFlannel)
		}
	}
	if !containsProvider(signals, ProviderCalico) {
		if hasDaemonSet(ctx, c, "calico-node") {
			signals = append(signals, ProviderCalico)
		}
	}

	// 4+5. Resolve signals.
	switch len(signals) {
	case 0:
		log.Info("no CNI detected, using iptables fallback")
		return Result{Provider: ProviderUnknown, NATBackend: NATBackendIPTables}, nil
	case 1:
		log.Info("CNI detected", "provider", signals[0])
		return resultFromProvider(signals[0]), nil
	default:
		log.Info("multiple CNIs detected, ambiguous", "providers", signals)
		return Result{Provider: ProviderUnknown, NATBackend: NATBackendIPTables, Ambiguous: true}, nil
	}
}

// hasCRD returns true if a CRD for the given group+resource exists in the cluster's REST mapper.
func hasCRD(c client.Client, group, resource string) bool {
	mappings, err := c.RESTMapper().ResourcesFor(schema.GroupVersionResource{
		Group:    group,
		Resource: resource,
	})
	return err == nil && len(mappings) > 0
}

// hasDaemonSet returns true if the named DaemonSet exists in kube-system.
// Returns false on any error (including 403 Forbidden) to remain graceful in
// clusters where the operator has minimal RBAC (design doc §9.4).
func hasDaemonSet(ctx context.Context, c client.Client, name string) bool {
	ds := &appsv1.DaemonSet{}
	err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: name}, ds)
	if err == nil {
		return true
	}
	if !apierrors.IsNotFound(err) {
		logf.FromContext(ctx).V(1).Info("DaemonSet check skipped", "name", name, "err", err)
	}
	return false
}

// resultFromProvider maps a Provider to its correct NAT backend.
func resultFromProvider(p Provider) Result {
	switch p {
	case ProviderCilium, ProviderCiliumKubeProxyFree:
		return Result{Provider: p, NATBackend: NATBackendNftables}
	default:
		return Result{Provider: p, NATBackend: NATBackendIPTables}
	}
}

func containsProvider(providers []Provider, p Provider) bool {
	for _, x := range providers {
		if x == p {
			return true
		}
	}
	return false
}
