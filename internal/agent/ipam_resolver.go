package agent

import (
	"context"
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func ciliumPoolGVK(version string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "cilium.io",
		Version: version,
		Kind:    "CiliumPodIPPool",
	}
}

func resolveAllocationSubnet(
	ctx context.Context, c ctrlclient.Client, impNet *impdevv1alpha1.ImpNetwork,
) (string, error) {
	if impNet.Spec.IPAM == nil || impNet.Spec.IPAM.Provider == "" || impNet.Spec.IPAM.Provider == "internal" {
		return impNet.Spec.Subnet, nil
	}
	if impNet.Spec.IPAM.Provider != "cilium" {
		return impNet.Spec.Subnet, nil
	}
	if c == nil {
		return "", fmt.Errorf("cilium ipam requires kubernetes client")
	}
	if impNet.Spec.IPAM.Cilium == nil || impNet.Spec.IPAM.Cilium.PoolRef == "" {
		return "", fmt.Errorf("cilium ipam requires spec.ipam.cilium.poolRef")
	}
	return resolveCiliumPoolCIDR(ctx, c, impNet.Spec.IPAM.Cilium.PoolRef)
}

func resolveCiliumPoolCIDR(ctx context.Context, c ctrlclient.Client, poolRef string) (string, error) {
	var lastErr error
	for _, version := range []string{"v2alpha1", "v2"} {
		pool := &unstructured.Unstructured{}
		pool.SetGroupVersionKind(ciliumPoolGVK(version))
		err := c.Get(ctx, ctrlclient.ObjectKey{Name: poolRef}, pool)
		if err != nil {
			lastErr = err
			if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
				continue
			}
			return "", fmt.Errorf("get CiliumPodIPPool %q: %w", poolRef, err)
		}

		if cidr := firstPoolCIDR(pool); cidr != "" {
			return cidr, nil
		}
		return "", fmt.Errorf("ciliumpodippool %q has no spec.cidrs entries", poolRef)
	}
	if lastErr == nil {
		return "", fmt.Errorf("get CiliumPodIPPool %q: not found", poolRef)
	}
	return "", fmt.Errorf("get CiliumPodIPPool %q: %w", poolRef, lastErr)
}

func firstPoolCIDR(pool *unstructured.Unstructured) string {
	cidrs, found, err := unstructured.NestedSlice(pool.Object, "spec", "cidrs")
	if err != nil || !found {
		return ""
	}
	for _, item := range cidrs {
		switch v := item.(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]any:
			if cidr, ok := v["cidr"].(string); ok && cidr != "" {
				return cidr
			}
		}
	}
	return ""
}
