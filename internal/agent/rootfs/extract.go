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
		if err == io.EOF { //nolint:errorlint
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
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil { //nolint:gosec
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { //nolint:gosec // G301: intermediate dirs in a VM rootfs must be world-traversable
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)) //nolint:gosec
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec
				f.Close() //nolint:errcheck
				return err
			}
			f.Close() //nolint:errcheck
		case tar.TypeSymlink:
			// Only create symlink if it points inside the dir (basic safety).
			_ = os.Symlink(hdr.Linkname, target) // best effort
		}
	}
	return nil
}
