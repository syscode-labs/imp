// internal/agent/rootfs/builder.go
package rootfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
// Blocks until the image is built. Subsequent calls with the same image digest return immediately.
func (b *Builder) Build(ctx context.Context, imageRef string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

// cachePath returns the expected cache file path for a given manifest digest hex string.
func (b *Builder) cachePath(digestHex string) string { //nolint:unused
	return filepath.Join(b.CacheDir, digestHex+".ext4")
}

// ensureCacheDir creates the cache directory if it does not exist.
func (b *Builder) ensureCacheDir() error { //nolint:unused
	return os.MkdirAll(b.CacheDir, 0o750)
}
