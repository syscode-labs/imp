//go:build linux

package main

import (
	"context"
	"fmt"
	"net"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// resolveVTEPIP returns the configured node-underlay address. An omitted
// profile is valid and keeps cross-node VXLAN disabled rather than guessing
// from status.hostIP, which can change when the runtime creates a guest bridge.
func resolveVTEPIP(ctx context.Context, client ctrlclient.Client, nodeName string) (string, error) {
	profile := &impdevv1alpha1.ClusterImpNodeProfile{}
	if err := client.Get(ctx, ctrlclient.ObjectKey{Name: nodeName}, profile); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("get ClusterImpNodeProfile %q: %w", nodeName, err)
	}
	if profile.Spec.VTEPIP == "" {
		return "", nil
	}
	if net.ParseIP(profile.Spec.VTEPIP).To4() == nil {
		return "", fmt.Errorf("ClusterImpNodeProfile %q has invalid vtepIP %q", nodeName, profile.Spec.VTEPIP)
	}
	return profile.Spec.VTEPIP, nil
}

// resolveVTEPIPDirect resolves VTEP IP using direct Kubernetes client without informers.
// This is used during startup when caches aren't ready yet.
func resolveVTEPIPDirect(ctx context.Context, config *rest.Config, nodeName string) (string, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("create dynamic client: %w", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    "imp.dev",
		Version:  "v1alpha1",
		Resource: "clusterimpnodeprofiles",
	}

	profile, err := dynamicClient.Resource(gvr).Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("get ClusterImpNodeProfile %q: %w", nodeName, err)
	}

	vtepIP, found, err := unstructured.NestedString(profile.Object, "spec", "vtepIP")
	if err != nil {
		return "", fmt.Errorf("get vtepIP from profile: %w", err)
	}
	if !found || vtepIP == "" {
		return "", nil
	}
	if net.ParseIP(vtepIP).To4() == nil {
		return "", fmt.Errorf("ClusterImpNodeProfile %q has invalid vtepIP %q", nodeName, vtepIP)
	}
	return vtepIP, nil
}
