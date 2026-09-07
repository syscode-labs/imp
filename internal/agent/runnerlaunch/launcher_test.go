//go:build linux

package runnerlaunch

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestDialWithRetryWaitsForRealConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	conn, err := (&Launcher{Sock: filepath.Join(t.TempDir(), "missing.vsock")}).dialWithRetry(ctx)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("dialWithRetry returned a connection before the VSOCK proxy was reachable")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dialWithRetry error = %v, want context deadline exceeded", err)
	}
}
