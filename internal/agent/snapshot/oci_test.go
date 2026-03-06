package snapshot_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"

	"github.com/syscode-labs/imp/internal/agent/snapshot"
)

func TestPushOCI_roundTrip(t *testing.T) {
	// Start an in-memory OCI registry.
	reg := registry.New()
	srv := httptest.NewServer(reg)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	statePath := filepath.Join(dir, "vm.state")
	memPath := filepath.Join(dir, "vm.mem")
	if err := os.WriteFile(statePath, []byte("state-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memPath, []byte("mem-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := strings.TrimPrefix(srv.URL, "http://") + "/test/snap"
	digest, err := snapshot.PushOCI(context.Background(), statePath, memPath, repo, "latest", nil)
	if err != nil {
		t.Fatalf("PushOCI: %v", err)
	}
	if digest == "" {
		t.Error("expected non-empty digest")
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Errorf("expected sha256: digest, got %q", digest)
	}
}
