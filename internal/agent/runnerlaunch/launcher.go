//go:build linux

// Package runnerlaunch hands a platform runner's one-time registration
// payload to a booted guest and launches the runner binary inside it.
//
// Flow: the pool controller mints a JIT config and stores it in a Secret
// referenced by ImpVM.spec.runnerConfigSecret. After the VM reaches Running,
// the agent dials the guest agent over VSOCK, sends an Exec for
// /usr/local/bin/runner with IMP_GITHUB_JITCONFIG in the command environment,
// and deletes the Secret only after the runner exits. The payload is one-time
// and must not be replayed after an ambiguous handoff.
package runnerlaunch

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	agentvsock "github.com/syscode-labs/imp/internal/agent/vsock"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
	"github.com/syscode-labs/imp/internal/runner"
)

var (
	jitConfigArgPattern  = regexp.MustCompile(`(?i)(--jitconfig(?:=|\s+))("[^"]*"|'[^']*'|\S+)`)
	credentialPattern    = regexp.MustCompile(`(?i)(\b(?:authorization\s*:\s*(?:bearer|basic)|bearer|basic)\s+)[^\s]+|\b(?:token|password|passwd|secret|api[_-]?key|access[_-]?token|client[_-]?secret)\s*[:=]\s*[^\s]+|\b(?:gh[pousr]_|github_pat_|glpat-|xox[baprs]-)[A-Za-z0-9._-]+`)
	credentialURLPattern = regexp.MustCompile(`(?i)https?://[^\s/@]+:[^\s/@]+@[^\s]+`)
)

const maxRunnerStderrLogBytes = 1024
const maxRunnerFailureReasonBytes = 256

func boundedFailureReason(reason, payload string) string {
	reason = sanitizeRunnerStderr(reason, payload)
	if len(reason) <= maxRunnerFailureReasonBytes {
		return reason
	}
	limit := maxRunnerFailureReasonBytes - 3
	for limit > 0 && (reason[limit]&0xc0) == 0x80 {
		limit--
	}
	return reason[:limit] + "..."
}

// sanitizeRunnerStderr keeps enough guest diagnostics to identify startup
// failures without copying one-time credentials into controller logs.
func sanitizeRunnerStderr(stderr, jitPayload string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	if jitPayload != "" {
		stderr = strings.ReplaceAll(stderr, jitPayload, "<redacted>")
	}
	stderr = jitConfigArgPattern.ReplaceAllString(stderr, `${1}<redacted>`)
	stderr = credentialURLPattern.ReplaceAllString(stderr, `<redacted-url>`)
	stderr = credentialPattern.ReplaceAllString(stderr, `<redacted>`)
	if len(stderr) <= maxRunnerStderrLogBytes {
		return stderr
	}
	// Keep the bound byte-based while avoiding a partial UTF-8 code point.
	limit := maxRunnerStderrLogBytes - 3
	for limit > 0 && (stderr[limit]&0xc0) == 0x80 {
		limit--
	}
	return stderr[:limit] + "..."
}

func runnerFailureStderr(exitCode int32, stderr, jitPayload string) string {
	if exitCode == 0 {
		return ""
	}
	return sanitizeRunnerStderr(stderr, jitPayload)
}

var launches sync.Map

const (
	// GuestVSOCKPort is the guest agent gRPC port inside the VM.
	GuestVSOCKPort = 10000

	// jitEnvVar is the environment variable the runner wrapper consumes.
	jitEnvVar = "IMP_GITHUB_JITCONFIG"

	// runnerBin is the runner entrypoint provided by the runner rootfs layer.
	runnerBin = "/usr/local/bin/runner"

	// bootDialTimeout bounds how long we wait for the guest agent to start
	// listening after the VM reaches Running.
	bootDialTimeout = 90 * time.Second

	// handoffTimeout bounds the runner's lifetime. GitHub one-shot runners
	// typically finish well within this; a stuck guest still expires via
	// spec.expireAfter. Generous because CI jobs can run for hours.
	handoffTimeout = 24 * time.Hour
)

// Result describes the outcome of a handoff attempt.
type Result struct {
	// HandoffDone is true when the payload was delivered (or was already
	// gone — a previous handoff completed).
	HandoffDone bool
	// ExitCode is the runner's exit code; valid only when HandoffDone and
	// Err == nil.
	ExitCode int32
	// Err is a terminal failure to launch the runner. Transient dial issues
	// are retried internally and do not surface here.
	Err error
}

// Launcher hands runner config payloads to guests. Create one per VM launch.
type Launcher struct {
	Client client.Client
	Reader client.Reader // uncached reader; avoids a cluster-wide Secret informer
	Sock   string        // path to the VM's VSOCK unix socket proxy
	Log    *slog.Logger
}

// Run performs the handoff and blocks until the runner exits (or fails).
// It is safe to call in a goroutine. Cancel ctx to abandon (the guest keeps
// running; Firecracker exit handling stays with the reconciler).
func (l *Launcher) Run(ctx context.Context, vm *impv1alpha1.ImpVM) Result {
	log := l.Log
	if log == nil {
		log = slog.Default()
	}
	secretName := vm.Spec.RunnerConfigSecret
	if secretName == "" {
		return Result{}
	}
	launchKey := vm.Namespace + "/" + vm.Name + "/" + string(vm.UID)
	if _, loaded := launches.LoadOrStore(launchKey, struct{}{}); loaded {
		return Result{}
	}
	defer launches.Delete(launchKey)

	// The accepted marker is written before Exec because the unary RPC has no
	// acknowledgement boundary: it returns only when the guest process exits.
	// A restart after this write is therefore ambiguous and must not replay.
	if vm.Status.RunnerExitCode != nil || vm.Status.RunnerHandoffAccepted {
		if vm.Status.RunnerExitCode != nil {
			return Result{HandoffDone: true, ExitCode: *vm.Status.RunnerExitCode}
		}
		return Result{Err: fmt.Errorf("runner handoff for VM UID %s is ambiguous; refusing replay", vm.UID)}
	}

	// Read the payload through the uncached APIReader by exact namespace/name.
	var secret corev1.Secret
	reader := l.Reader
	if reader == nil {
		reader = l.Client
	}
	if reader == nil || l.Client == nil {
		return Result{Err: fmt.Errorf("runner launcher requires Kubernetes reader and client")}
	}
	err := reader.Get(ctx, client.ObjectKey{Namespace: vm.Namespace, Name: secretName}, &secret)
	if err != nil {
		l.recordFailure(ctx, vm, err.Error(), "")
		// NotFound is intentionally not interpreted as a completed handoff: it
		// may be a cleanup race or an agent restart in an unknown state.
		return Result{Err: fmt.Errorf("get runner config Secret %s: %w", secretName, err)}
	}
	config, err := runner.JITConfigFromSecretValue(
		secret.Annotations[runner.JITConfigVersionAnnotation],
		secret.Data[runner.JITConfigSecretKey],
		secret.Annotations[runner.JITConfigRunnerAnnotation],
	)
	if err != nil {
		l.recordFailure(ctx, vm, err.Error(), "")
		return Result{Err: fmt.Errorf("decode runner config Secret %s: %w", secretName, err)}
	}

	// Dial the guest agent, retrying while the guest finishes booting.
	conn, err := l.dialWithRetry(ctx)
	if err != nil {
		// The guest has not received the payload yet, so a later reconciliation
		// may safely retry the handoff.
		l.recordFailure(ctx, vm, err.Error(), config.EncodedConfig)
		return Result{Err: fmt.Errorf("dial guest agent: %w", err)}
	}
	defer conn.Close() //nolint:errcheck
	guest := pb.NewGuestAgentClient(conn)

	// Record acceptance only after the guest boundary is reachable and
	// immediately before Exec. A dial failure must remain retryable, while a
	// restart after this marker is necessarily ambiguous because Exec is unary:
	// its error may arrive after the guest has started the process.
	if err := l.recordAccepted(ctx, vm); err != nil {
		return Result{Err: fmt.Errorf("record runner handoff acceptance: %w", err)}
	}

	// Hand off. Env keeps the payload out of argv (not visible in guest ps).
	hCtx, cancel := context.WithTimeout(ctx, handoffTimeout)
	defer cancel()
	resp, err := guest.Exec(hCtx, &pb.ExecRequest{
		Command: []string{runnerBin},
		Env: map[string]string{
			jitEnvVar:                config.EncodedConfig,
			"RUNNER_ALLOW_RUNASROOT": "1",
		},
		TimeoutSeconds: int32(handoffTimeout / time.Second),
	})
	if err != nil {
		l.recordFailure(ctx, vm, err.Error(), config.EncodedConfig)
		// Acceptance makes the handoff non-replayable even when the unary RPC
		// returns an ambiguous transport error. Delete the payload here too so
		// the credential cannot remain available until Secret expiry.
		if delErr := l.Client.Delete(ctx, &secret); delErr != nil && !apierrors.IsNotFound(delErr) {
			log.Error("failed to delete runner config Secret after ambiguous handoff",
				"secret", secretName, "err", delErr)
		}
		return Result{Err: fmt.Errorf("launch runner in guest: %w", err)}
	}

	if resp.ExitCode != 0 {
		if stderr := runnerFailureStderr(resp.ExitCode, resp.Stderr, config.EncodedConfig); stderr != "" {
			log.Warn("runner emitted stderr", "stderr", stderr)
		}
	}
	if err := l.recordOutcome(ctx, vm, resp.ExitCode, runnerFailureStderr(resp.ExitCode, resp.Stderr, config.EncodedConfig)); err != nil {
		log.Error("failed to persist runner handoff outcome", "err", err)
	}

	// The Secret remains until unary Exec returns, which is the runner-exit
	// boundary. Delete-NotFound is an idempotent cleanup success.
	delErr := l.Client.Delete(ctx, &secret)
	if delErr != nil && !apierrors.IsNotFound(delErr) {
		log.Error("failed to delete runner config Secret after runner exit",
			"secret", secretName, "err", delErr)
	}

	log.Info("runner exited", "exit_code", resp.ExitCode, "secret", secretName)
	if resp.ExitCode != 0 {
		return Result{HandoffDone: true, ExitCode: resp.ExitCode,
			Err: fmt.Errorf("runner exited with code %d", resp.ExitCode)}
	}
	return Result{HandoffDone: true, ExitCode: resp.ExitCode}
}

func (l *Launcher) recordAccepted(ctx context.Context, vm *impv1alpha1.ImpVM) error {
	base := vm.DeepCopy()
	vm.Status.RunnerHandoffAccepted = true
	return l.Client.Status().Patch(ctx, vm, client.MergeFrom(base))
}

func (l *Launcher) recordFailure(ctx context.Context, vm *impv1alpha1.ImpVM, reason, payload string) {
	base := vm.DeepCopy()
	vm.Status.RunnerFailureReason = boundedFailureReason(reason, payload)
	if err := l.Client.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		if l.Log != nil {
			l.Log.Error("failed to persist runner handoff failure", "err", err)
		}
	}
}

func (l *Launcher) recordOutcome(ctx context.Context, vm *impv1alpha1.ImpVM, exitCode int32, reason string) error {
	base := vm.DeepCopy()
	vm.Status.RunnerExitCode = &exitCode
	vm.Status.RunnerFailureReason = boundedFailureReason(reason, "")
	return l.Client.Status().Patch(ctx, vm, client.MergeFrom(base))
}

// dialWithRetry waits for the lazy gRPC client to establish a real HTTP/2
// connection. Creating a grpc.ClientConn alone does not call the VSOCK dialer.
func (l *Launcher) dialWithRetry(ctx context.Context) (*grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, bootDialTimeout)
	defer cancel()

	conn, err := agentvsock.Dial(dialCtx, l.Sock, GuestVSOCKPort)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return conn, nil
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			_ = conn.Close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("guest agent not ready within %s: %w", bootDialTimeout, dialCtx.Err())
		}
	}
}
