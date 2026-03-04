//go:build linux

package rootfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syscode-labs/imp/internal/agent/rootfs"
)

func TestBuildOption_WithGuestAgent(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("rootfs injection requires root (loop mount)")
	}
	// Write a fake guest agent binary
	agentBin, err := os.CreateTemp("", "fake-agent-*")
	if err != nil {
		t.Fatal(err)
	}
	agentBin.WriteString("#!/bin/sh\necho guest-agent")
	agentBin.Chmod(0755)
	agentBin.Close()
	defer os.Remove(agentBin.Name())

	// Build a minimal ext4 for testing
	tmpDir := t.TempDir()
	ext4Path := filepath.Join(tmpDir, "test.ext4")
	if err := rootfs.BuildMinimalExt4ForTest(tmpDir, ext4Path, 32); err != nil {
		t.Fatal(err)
	}

	// Inject
	if err := rootfs.InjectGuestAgent(agentBin.Name(), ext4Path); err != nil {
		t.Fatal(err)
	}

	// Mount and verify files exist
	mnt := t.TempDir()
	if err := rootfs.MountExt4(ext4Path, mnt); err != nil {
		t.Fatal(err)
	}
	defer rootfs.UmountExt4(mnt) //nolint:errcheck

	if _, err := os.Stat(filepath.Join(mnt, ".imp", "guest-agent")); err != nil {
		t.Errorf(".imp/guest-agent not found after injection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mnt, ".imp", "init")); err != nil {
		t.Errorf(".imp/init not found after injection: %v", err)
	}
}
