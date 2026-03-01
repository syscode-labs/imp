// internal/agent/rootfs/builder.go
package rootfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Builder builds ext4 rootfs disk images from OCI images.
// Results are cached by manifest digest — repeated calls with the same image are instant.
type Builder struct {
	// CacheDir is the directory where ext4 images are stored.
	// Example: /var/lib/imp/images
	CacheDir string

	// Insecure allows connecting to registries over plain HTTP.
	// Set true only in tests that use httptest servers.
	Insecure bool
}

// Build returns the path to a ready-to-use ext4 image for imageRef.
// Blocks until the image is built. Subsequent calls with the same manifest digest return immediately.
func (b *Builder) Build(ctx context.Context, imageRef string) (string, error) {
	// 1. Fetch image manifest (layers are downloaded lazily).
	img, err := b.pullImage(ctx, imageRef)
	if err != nil {
		return "", err
	}

	// 2. Resolve manifest digest → cache key.
	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("digest: %w", err)
	}

	// 3. Check cache — return immediately if already built.
	dest := b.cachePath(digest.Hex)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}

	// 4. Ensure cache directory exists.
	if err := b.ensureCacheDir(); err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}

	// 5. Extract all image layers into a temp directory.
	tmpDir, err := os.MkdirTemp("", "imp-rootfs-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	if err := extractLayers(img, tmpDir); err != nil {
		return "", fmt.Errorf("extract layers: %w", err)
	}

	// 6. Write /sbin/init from CMD/ENTRYPOINT.
	if err := writeInit(img, tmpDir); err != nil {
		return "", fmt.Errorf("write init: %w", err)
	}

	// 7. Calculate size + 64 MiB headroom, then assemble ext4.
	size, err := dirSize(tmpDir)
	if err != nil {
		return "", fmt.Errorf("dir size: %w", err)
	}
	sizeMiB := size/(1024*1024) + 64

	// Write to a temp file first, then atomically rename to the cache path.
	// This prevents a partially-written file from poisoning the cache.
	tmpExt4 := dest + ".tmp"
	if err := buildExt4(ctx, tmpDir, tmpExt4, sizeMiB); err != nil {
		os.Remove(tmpExt4) //nolint:errcheck
		return "", fmt.Errorf("build ext4: %w", err)
	}

	if err := os.Rename(tmpExt4, dest); err != nil {
		os.Remove(tmpExt4) //nolint:errcheck
		return "", fmt.Errorf("rename to cache: %w", err)
	}

	return dest, nil
}

// cachePath returns the expected cache file path for a given manifest digest hex string.
func (b *Builder) cachePath(digestHex string) string {
	return filepath.Join(b.CacheDir, digestHex+".ext4")
}

// ensureCacheDir creates the cache directory if it does not exist.
func (b *Builder) ensureCacheDir() error {
	return os.MkdirAll(b.CacheDir, 0o750)
}

// pullImage fetches the image manifest from the registry.
// Layer data is not downloaded here — go-containerregistry fetches layers lazily.
func (b *Builder) pullImage(ctx context.Context, imageRef string) (v1.Image, error) {
	var opts []name.Option
	if b.Insecure {
		opts = append(opts, name.Insecure)
	}
	ref, err := name.ParseReference(imageRef, opts...)
	if err != nil {
		return nil, fmt.Errorf("parse reference %q: %w", imageRef, err)
	}
	img, err := remote.Image(ref, remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("fetch image %q: %w", imageRef, err)
	}
	return img, nil
}
