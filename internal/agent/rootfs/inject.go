package rootfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	// GuestAgentContainerPath is where the guest agent binary lives inside the imp-agent container.
	GuestAgentContainerPath = "/opt/imp/guest-agent"

	// initScript is written as /.imp/init inside the VM rootfs.
	initScript = "#!/bin/sh\n[ -f /.imp/env ] && . /.imp/env\n/.imp/guest-agent &\nexec /sbin/init \"$@\"\n"
)

// BuildOption is applied to the extracted rootfs directory before building ext4.
// CacheKey must uniquely identify output differences introduced by the option.
type BuildOption interface {
	Apply(tmpDir string) error
	CacheKey() string
}

type buildOption struct {
	key   string
	apply func(tmpDir string) error
}

func (o buildOption) Apply(tmpDir string) error { return o.apply(tmpDir) }
func (o buildOption) CacheKey() string          { return o.key }

// WithGuestAgent injects the guest agent binary and init wrapper into the rootfs tmpDir.
// guestAgentSrc is the host path to the guest agent binary.
func WithGuestAgent(guestAgentSrc string) BuildOption {
	return buildOption{
		key: "ga",
		apply: func(tmpDir string) error {
			impDir := filepath.Join(tmpDir, ".imp")
			if err := os.MkdirAll(impDir, 0o755); err != nil { //nolint:gosec // G301: rootfs dir must be world-executable for VM init
				return err
			}
			if err := copyFile(guestAgentSrc, filepath.Join(impDir, "guest-agent"), 0o755); err != nil {
				return fmt.Errorf("inject guest-agent: %w", err)
			}
			if err := os.WriteFile(filepath.Join(impDir, "init"), []byte(initScript), 0o755); err != nil { //nolint:gosec // G306: init script must be executable
				return fmt.Errorf("inject init: %w", err)
			}
			return nil
		},
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // G304: caller controls src path
	if err != nil {
		return err
	}
	defer in.Close()                                                       //nolint:errcheck
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // G304: caller controls dst path
	if err != nil {
		return err
	}
	defer out.Close() //nolint:errcheck
	_, err = io.Copy(out, in)
	return err
}
