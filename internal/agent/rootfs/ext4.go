// internal/agent/rootfs/ext4.go
package rootfs

import (
	"context"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
)

// buildExt4 assembles an ext4 image from dir, writing the result to dest.
// sizeMiB is the size of the output image in mebibytes.
func buildExt4(ctx context.Context, dir, dest string, sizeMiB int64) error {
	bin, err := mke2fsBin()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, //nolint:gosec // G204: binary path resolved via LookPath
		"-t", "ext4",
		"-d", dir,
		"-F", // force (overwrite if exists)
		dest,
		fmt.Sprintf("%dm", sizeMiB),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mke2fs: %w\n%s", err, out)
	}
	return nil
}

// mke2fsBin returns the path to mke2fs or mkfs.ext4, whichever is found first.
func mke2fsBin() (string, error) {
	if p, err := exec.LookPath("mke2fs"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("mkfs.ext4"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("neither mke2fs nor mkfs.ext4 found in PATH")
}

// dirSize returns the total size in bytes of all regular files under dir.
// Used to calculate the minimum ext4 image size.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// Ensure dirSize is referenced to suppress unused lint errors until it is
// wired into Build() in a subsequent task.
var _ = dirSize
