// Package snapshot provides helpers for pushing Firecracker snapshot artifacts
// as two-layer OCI images compatible with standard registry tooling and Spegel.
package snapshot

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	corev1 "k8s.io/api/core/v1"
)

// PushOCI packages statePath (layer 1) and memPath (layer 2) as a two-layer OCI
// image and pushes it to repository:tag.
//
// pullSecretRef is reserved for future registry credential support and is currently
// unused (anonymous auth via the default keychain is used).
//
// Returns the manifest digest (e.g. "sha256:abc123...").
func PushOCI(ctx context.Context, statePath, memPath, repository, tag string, _ *corev1.LocalObjectReference) (string, error) {
	ref, err := name.ParseReference(fmt.Sprintf("%s:%s", repository, tag), name.Insecure)
	if err != nil {
		return "", fmt.Errorf("parse reference: %w", err)
	}

	stateLayer, err := tarball.LayerFromFile(statePath)
	if err != nil {
		return "", fmt.Errorf("state layer: %w", err)
	}
	memLayer, err := tarball.LayerFromFile(memPath)
	if err != nil {
		return "", fmt.Errorf("mem layer: %w", err)
	}

	img, err := mutate.AppendLayers(empty.Image, stateLayer, memLayer)
	if err != nil {
		return "", fmt.Errorf("append layers: %w", err)
	}

	if err := remote.Write(ref, img, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
		return "", fmt.Errorf("push image: %w", err)
	}

	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("get digest: %w", err)
	}
	return digest.String(), nil
}
