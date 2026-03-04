//go:build linux

package rootfs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// InjectGuestAgent copies the guestAgentSrc binary and the init wrapper into the
// ext4 image at ext4Path. Requires root (uses loop mount).
func InjectGuestAgent(guestAgentSrc, ext4Path string) error {
	mnt, err := os.MkdirTemp("", "imp-inject-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mnt) //nolint:errcheck

	if err := MountExt4(ext4Path, mnt); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	defer UmountExt4(mnt) //nolint:errcheck

	impDir := filepath.Join(mnt, ".imp")
	if err := os.MkdirAll(impDir, 0o755); err != nil {
		return err
	}

	dst := filepath.Join(impDir, "guest-agent")
	if err := copyFile(guestAgentSrc, dst, 0o755); err != nil {
		return fmt.Errorf("copy guest-agent: %w", err)
	}

	initPath := filepath.Join(impDir, "init")
	if err := os.WriteFile(initPath, []byte(initScript), 0o755); err != nil { //nolint:gosec // G306: init script must be executable
		return fmt.Errorf("write init: %w", err)
	}
	return nil
}

// MountExt4 mounts ext4Path at mnt using a loop device.
func MountExt4(ext4Path, mnt string) error {
	out, err := exec.Command("mount", "-o", "loop", ext4Path, mnt).CombinedOutput() //nolint:gosec // G204: fixed args
	if err != nil {
		return fmt.Errorf("mount: %w\n%s", err, out)
	}
	return nil
}

// UmountExt4 unmounts mnt.
func UmountExt4(mnt string) error {
	out, err := exec.Command("umount", mnt).CombinedOutput() //nolint:gosec // G204: fixed arg
	if err != nil {
		return fmt.Errorf("umount: %w\n%s", err, out)
	}
	return nil
}

// BuildMinimalExt4ForTest creates a tiny ext4 image for use in tests.
// sizeMiB is the size in MiB.
func BuildMinimalExt4ForTest(dir, dest string, sizeMiB int64) error {
	return buildExt4(context.Background(), dir, dest, sizeMiB)
}
