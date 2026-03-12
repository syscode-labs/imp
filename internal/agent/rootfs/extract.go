// internal/agent/rootfs/extract.go
package rootfs

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
)

// extractLayers squashes all image layers and extracts the merged filesystem into dir.
func extractLayers(img v1.Image, dir string) error {
	rc := mutate.Extract(img)
	defer rc.Close() //nolint:errcheck
	return untar(dir, rc)
}

// untar extracts a tar stream into dir, skipping whiteout files and preventing path traversal.
func untar(dir string, r io.Reader) error {
	dir = filepath.Clean(dir)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF { //nolint:errorlint // tar.Reader returns io.EOF directly, never wrapped
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		// Skip Docker whiteout files (layer deletion markers).
		if strings.HasPrefix(filepath.Base(hdr.Name), ".wh.") {
			continue
		}

		target := filepath.Join(dir, hdr.Name) //nolint:gosec // G305: path traversal guard follows immediately below

		// Prevent path traversal: target must be inside dir.
		if !strings.HasPrefix(filepath.Clean(target), dir+string(os.PathSeparator)) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil { //nolint:gosec // G115: tar header mode is a standard uint32→FileMode cast
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { //nolint:gosec // G301: intermediate dirs in a VM rootfs must be world-traversable
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)) //nolint:gosec // G115: tar header mode is a standard uint32→FileMode cast
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // G110: decompression bomb risk accepted; caller controls the OCI image source
				f.Close() //nolint:errcheck
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Validate the symlink target does not escape dir.
			// Resolve relative targets against the symlink's own directory.
			linkTarget := resolveSymlinkTarget(target, hdr.Linkname, dir)
			if !pathWithinDir(dir, linkTarget) {
				continue // symlink target escapes rootfs directory — skip
			}
			_ = os.Symlink(hdr.Linkname, target) // best effort
		case tar.TypeLink:
			linkTarget := filepath.Join(dir, hdr.Linkname) //nolint:gosec // G305: validated before linking
			if !pathWithinDir(dir, linkTarget) {
				continue // hardlink target escapes rootfs directory — skip
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { //nolint:gosec // G301: rootfs dirs must be world-traversable
				return err
			}
			_ = os.Link(linkTarget, target) //nolint:errcheck // best effort for tar hardlinks
		}
	}
	return nil
}

func resolveSymlinkTarget(linkPath, linkName, rootDir string) string {
	// Absolute symlinks in image layers are rootfs-absolute, not host-absolute.
	// Rebase them under the extraction root for safety validation.
	if filepath.IsAbs(linkName) {
		return filepath.Join(rootDir, strings.TrimPrefix(filepath.Clean(linkName), string(os.PathSeparator)))
	}
	return filepath.Join(filepath.Dir(linkPath), linkName)
}

func pathWithinDir(rootDir, path string) bool {
	root := filepath.Clean(rootDir)
	clean := filepath.Clean(path)
	return clean == root || strings.HasPrefix(clean, root+string(os.PathSeparator))
}
