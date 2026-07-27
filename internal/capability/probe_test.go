package capability

import (
	"os"
	"path/filepath"
	"testing"
)

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
}

func TestCheck_Success(t *testing.T) {
	dir := t.TempDir()
	kvmPath := filepath.Join(dir, "kvm")
	binPath := filepath.Join(dir, "firecracker")
	if err := os.WriteFile(kvmPath, nil, 0o666); err != nil {
		t.Fatalf("write kvm device stub: %v", err)
	}
	writeExecutable(t, binPath)

	got := Check(kvmPath, binPath)

	if !got.OK() {
		t.Fatalf("expected OK, got %+v", got)
	}
	if !got.KVMAvailable || got.KVMError != "" {
		t.Errorf("expected KVM available with no error, got %+v", got)
	}
	if !got.FirecrackerAvailable || got.FirecrackerError != "" {
		t.Errorf("expected Firecracker available with no error, got %+v", got)
	}
}

func TestCheck_MissingDevice(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "firecracker")
	writeExecutable(t, binPath)

	got := Check(filepath.Join(dir, "no-such-kvm"), binPath)

	if got.OK() {
		t.Fatalf("expected not OK, got %+v", got)
	}
	if got.KVMAvailable || got.KVMError == "" {
		t.Errorf("expected KVM unavailable with an error, got %+v", got)
	}
	if !got.FirecrackerAvailable {
		t.Errorf("expected Firecracker still available, got %+v", got)
	}
}

func TestCheck_MissingBinary(t *testing.T) {
	dir := t.TempDir()
	kvmPath := filepath.Join(dir, "kvm")
	if err := os.WriteFile(kvmPath, nil, 0o666); err != nil {
		t.Fatalf("write kvm device stub: %v", err)
	}

	got := Check(kvmPath, filepath.Join(dir, "no-such-firecracker"))

	if got.OK() {
		t.Fatalf("expected not OK, got %+v", got)
	}
	if !got.KVMAvailable {
		t.Errorf("expected KVM still available, got %+v", got)
	}
	if got.FirecrackerAvailable || got.FirecrackerError == "" {
		t.Errorf("expected Firecracker unavailable with an error, got %+v", got)
	}
}

func TestCheck_NonExecutableBinary(t *testing.T) {
	dir := t.TempDir()
	kvmPath := filepath.Join(dir, "kvm")
	binPath := filepath.Join(dir, "firecracker")
	if err := os.WriteFile(kvmPath, nil, 0o666); err != nil {
		t.Fatalf("write kvm device stub: %v", err)
	}
	if err := os.WriteFile(binPath, []byte("not executable"), 0o644); err != nil {
		t.Fatalf("write non-executable binary: %v", err)
	}

	got := Check(kvmPath, binPath)

	if got.FirecrackerAvailable {
		t.Errorf("expected non-executable binary to fail, got %+v", got)
	}
}
