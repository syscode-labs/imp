package main

import (
	"context"
	"fmt"
	"net"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
