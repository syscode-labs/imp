//go:build linux

package runnerlaunch

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
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

func TestRunnerOutcomeIsNonSecretAndRestartSafe(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	vm := &impv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: "vm", Namespace: "ns", UID: "vm-uid"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vm).WithStatusSubresource(vm).Build()
	launcher := &Launcher{Client: c}
	if err := launcher.recordAccepted(context.Background(), vm); err != nil {
		t.Fatalf("recordAccepted: %v", err)
	}
	if !vm.Status.RunnerHandoffAccepted {
		t.Fatal("handoff acceptance was not recorded")
	}
	launcher.recordFailure(context.Background(), vm, "runner failed token=secret-value", "secret-value")
	if strings.Contains(vm.Status.RunnerFailureReason, "secret-value") || strings.Contains(vm.Status.RunnerFailureReason, "token=") {
		t.Fatalf("failure reason leaked payload: %q", vm.Status.RunnerFailureReason)
	}
	code := int32(17)
	if err := launcher.recordOutcome(context.Background(), vm, code, "runner exited"); err != nil {
		t.Fatalf("recordOutcome: %v", err)
	}
	stored := &impv1alpha1.ImpVM{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(vm), stored); err != nil {
		t.Fatalf("get stored VM: %v", err)
	}
	if !stored.Status.RunnerHandoffAccepted {
		t.Fatal("stored VM lost the durable handoff marker")
	}
	if stored.Status.RunnerExitCode == nil || *stored.Status.RunnerExitCode != code {
		t.Fatalf("stored exit code = %v, want %d", stored.Status.RunnerExitCode, code)
	}
}

func TestRunnerHandoffMarkerRefusesReplay(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	vm := &impv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: "vm", Namespace: "ns", UID: "vm-uid"},
		Spec:       impv1alpha1.ImpVMSpec{RunnerConfigSecret: "jitconfig"},
		Status:     impv1alpha1.ImpVMStatus{RunnerHandoffAccepted: true},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vm).WithStatusSubresource(vm).Build()
	result := (&Launcher{Client: c}).Run(context.Background(), vm)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "refusing replay") {
		t.Fatalf("Run() error = %v, want durable replay refusal", result.Err)
	}
}
