//go:build linux

// Package runnerlaunch hands a platform runner's one-time registration
// payload to a booted guest and launches the runner binary inside it.
//
// Flow: the pool controller mints a JIT config and stores it in a Secret
// referenced by ImpVM.spec.runnerConfigSecret. After the VM reaches Running,
// the agent dials the guest agent over VSOCK, sends an Exec for
// /usr/local/bin/runner with IMP_GITHUB_JITCONFIG in the command environment,
// and deletes the Secret — the payload is one-time and must not outlive
// handoff. If the Secret is already gone (agent restart after a successful
// handoff) the launcher is a no-op.
package runnerlaunch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	agentvsock "github.com/syscode-labs/imp/internal/agent/vsock"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
	"github.com/syscode-labs/imp/internal/runner"
)

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

	// Read the payload. Missing Secret after an agent restart means the
	// handoff already happened; treat as done.
	var secret corev1.Secret
	reader := l.Reader
	if reader == nil {
		reader = l.Client
	}
	if reader == nil || l.Client == nil {
		return Result{Err: fmt.Errorf("runner launcher requires Kubernetes reader and client")}
	}
	err := reader.Get(ctx, client.ObjectKey{Namespace: vm.Namespace, Name: secretName}, &secret)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("runner config Secret already deleted; handoff previously completed", "secret", secretName)
		return Result{HandoffDone: true}
	case err != nil:
		return Result{Err: fmt.Errorf("get runner config Secret %s: %w", secretName, err)}
	}
	config, err := runner.JITConfigFromSecretValue(
		secret.Annotations[runner.JITConfigVersionAnnotation],
		secret.Data[runner.JITConfigSecretKey],
		secret.Annotations[runner.JITConfigRunnerAnnotation],
	)
	if err != nil {
		return Result{Err: fmt.Errorf("decode runner config Secret %s: %w", secretName, err)}
	}

	// Dial the guest agent, retrying while the guest finishes booting.
	conn, err := l.dialWithRetry(ctx)
	if err != nil {
		return Result{Err: fmt.Errorf("dial guest agent: %w", err)}
	}
	defer conn.Close() //nolint:errcheck
	guest := pb.NewGuestAgentClient(conn)

	// Hand off. env keeps the payload out of argv (not visible in guest ps).
	hCtx, cancel := context.WithTimeout(ctx, handoffTimeout)
	defer cancel()
	resp, err := guest.Exec(hCtx, &pb.ExecRequest{
		Command: []string{runnerBin},
		Env:     map[string]string{jitEnvVar: config.EncodedConfig},
	})
	if err != nil {
		return Result{Err: fmt.Errorf("launch runner in guest: %w", err)}
	}

	// One-time payload: delete immediately after successful delivery.
	delErr := l.Client.Delete(ctx, &secret)
	if delErr != nil && !apierrors.IsNotFound(delErr) {
		// Non-fatal: expiry and pool cleanup will collect it, but surface it.
		log.Error("failed to delete runner config Secret after handoff",
			"secret", secretName, "err", delErr)
	}

	log.Info("runner exited", "exit_code", resp.ExitCode, "secret", secretName)
	if resp.ExitCode != 0 {
		return Result{HandoffDone: true, ExitCode: resp.ExitCode,
			Err: fmt.Errorf("runner exited with code %d", resp.ExitCode)}
	}
	return Result{HandoffDone: true, ExitCode: resp.ExitCode}
}

// dialWithRetry waits for the guest agent to accept VSOCK connections.
func (l *Launcher) dialWithRetry(ctx context.Context) (*grpc.ClientConn, error) {
	deadline := time.Now().Add(bootDialTimeout)
	var lastErr error
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("guest agent not ready within %s: %w", bootDialTimeout, lastErr)
		}
		conn, err := agentvsock.Dial(ctx, l.Sock, GuestVSOCKPort)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
