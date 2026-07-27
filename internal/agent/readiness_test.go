//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCapabilityReadyzCheck(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "firecracker")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write firecracker stub: %v", err)
	}

	check := CapabilityReadyzCheck(binPath)
	// No /dev/kvm on most CI/dev hosts, so the check must fail closed.
	if err := check(nil); err == nil {
		t.Fatalf("expected error without /dev/kvm, got nil")
	}

	missing := CapabilityReadyzCheck(filepath.Join(dir, "no-such-binary"))
	if err := missing(nil); err == nil {
		t.Fatalf("expected error for missing firecracker binary, got nil")
	}
}
