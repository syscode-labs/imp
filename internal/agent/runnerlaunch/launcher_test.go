//go:build linux

package runnerlaunch

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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

func TestSanitizeRunnerStderr(t *testing.T) {
	tests := []struct {
		name, stderr, payload, want string
	}{
		{name: "benign", stderr: "config.sh: runner binary not found", want: "config.sh: runner binary not found"},
		{name: "jit payload", stderr: "config.sh failed: encoded=JIT_PAYLOAD", payload: "JIT_PAYLOAD", want: "config.sh failed: encoded=<redacted>"},
		{name: "jitconfig argument", stderr: "runner: --jitconfig=secret-value rejected", want: "runner: --jitconfig=<redacted> rejected"},
		{name: "credentials and tokens", stderr: "token=abc password:xyz Authorization: Bearer ghp_secret github_pat_foo glpat-bar", want: "<redacted> <redacted> <redacted> <redacted> <redacted>"},
		{name: "credential URL", stderr: "fetch https://user:password@example.com/path failed", want: "fetch <redacted-url> failed"},
		{name: "empty", stderr: " \n	", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeRunnerStderr(tt.stderr, tt.payload); got != tt.want {
				t.Fatalf("sanitizeRunnerStderr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeRunnerStderrTruncates(t *testing.T) {
	got := sanitizeRunnerStderr(strings.Repeat("x", maxRunnerStderrLogBytes+100), "")
	if len(got) != maxRunnerStderrLogBytes {
		t.Fatalf("sanitized stderr length = %d, want %d", len(got), maxRunnerStderrLogBytes)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("sanitized stderr does not have truncation suffix: %q", got[len(got)-10:])
	}
}

func TestSanitizeRunnerStderrSuccessDoesNotLog(t *testing.T) {
	if got := runnerFailureStderr(0, "runner succeeded with token=abc", ""); got != "" {
		t.Fatalf("success stderr = %q, want empty", got)
	}
	if got := runnerFailureStderr(1, "", ""); got != "" {
		t.Fatalf("empty failure stderr = %q, want empty", got)
	}
}
