package rootfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const (
	// GuestAgentContainerPath is where the guest agent binary lives inside the imp-agent container.
	GuestAgentContainerPath = "/opt/imp/guest-agent"

	virtualFilesystemMounts = "mkdir -p /proc /sys /dev/pts /run\nmount -t proc proc /proc\nmount -t sysfs sysfs /sys\nmount -t devpts devpts /dev/pts\nmount -t tmpfs tmpfs /run\n"

	// initScript is written as /.imp/init inside the VM rootfs.
	initScript = "#!/bin/sh\n" + virtualFilesystemMounts + "[ -f /.imp/env ] && . /.imp/env\n/.imp/guest-agent &\nexec /sbin/init \"$@\"\n"

	// runnerInitScript keeps the guest agent as PID 1. The runner is started
	// later through the guest Exec API after its one-time JIT config arrives.
	runnerInitScript = "#!/bin/sh\n" + virtualFilesystemMounts + "[ -f /.imp/env ] && . /.imp/env\nexec /.imp/guest-agent\n"
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

type diskSizeOption struct {
	diskMiB int64
}

func (o diskSizeOption) Apply(string) error { return nil }
func (o diskSizeOption) CacheKey() string   { return "disk-" + strconv.FormatInt(o.diskMiB, 10) }
func (o diskSizeOption) DiskSizeMiB() int64 { return o.diskMiB }

// WithDiskSizeGiB sets the fixed size of the rootfs image.
func WithDiskSizeGiB(diskGiB int32) BuildOption {
	return diskSizeOption{diskMiB: int64(diskGiB) * 1024}
}

// WithGuestAgent injects the guest agent binary and init wrapper into the rootfs tmpDir.
// guestAgentSrc is the host path to the guest agent binary.
func WithGuestAgent(guestAgentSrc string) BuildOption {
	return withGuestAgent(guestAgentSrc, "ga-v2", initScript)
}

// WithRunnerGuestAgent injects the guest agent as PID 1 for JIT runner VMs.
// Its distinct cache key prevents reuse of rootfs images that start the OCI
// entrypoint before the runner's one-time config has been delivered.
func WithRunnerGuestAgent(guestAgentSrc string) BuildOption {
	return withGuestAgent(guestAgentSrc, "ga-runner-v2", runnerInitScript)
}

func withGuestAgent(guestAgentSrc, cacheKey, script string) BuildOption {
	content, err := os.ReadFile(guestAgentSrc) //nolint:gosec // G304: caller controls source path
	if err != nil {
		// Apply will report the source error. Keep a path-derived key here so an
		// unreadable source cannot accidentally share a known-good cache entry.
		pathDigest := sha256.Sum256([]byte(guestAgentSrc))
		cacheKey += "-unreadable-" + hex.EncodeToString(pathDigest[:])
	} else {
		contentDigest := sha256.Sum256(content)
		cacheKey += "-" + hex.EncodeToString(contentDigest[:])
	}
	return buildOption{
		key: cacheKey,
		apply: func(tmpDir string) error {
			impDir := filepath.Join(tmpDir, ".imp")
			if err := os.MkdirAll(impDir, 0o755); err != nil { //nolint:gosec // G301: rootfs dir must be world-executable for VM init
				return err
			}
			if err := copyFile(guestAgentSrc, filepath.Join(impDir, "guest-agent"), 0o755); err != nil {
				return fmt.Errorf("inject guest-agent: %w", err)
			}
			if err := os.WriteFile(filepath.Join(impDir, "init"), []byte(script), 0o755); err != nil { //nolint:gosec // G306: init script must be executable
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
