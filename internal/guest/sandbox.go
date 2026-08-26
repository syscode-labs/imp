//go:build linux

// Package guest hosts the in-VM gRPC services.
package guest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	pb "github.com/syscode-labs/imp/internal/proto/sandbox"
)

// envSandboxControlToken names the environment variable holding the expected
// sandbox control token. When unset or empty, every SandboxControl call is
// refused and the surface stays inert — ordinary ImpVM workloads are unaffected.
const envSandboxControlToken = "IMP_SANDBOX_CONTROL_TOKEN"

// ErrSandboxDisabled is returned when the control token is not configured.
var ErrSandboxDisabled = errors.New("sandbox control sessions are not enabled")

// ErrUnknownSession is returned when a call presents an unopened session id.
var ErrUnknownSession = errors.New("unknown sandbox session")

// SandboxServer implements the SandboxControl gRPC service. Sessions exist
// only after OpenSession succeeds with the configured token, so without
// configuration the service is a no-op surface compiled into the binary.
type SandboxServer struct {
	pb.UnimplementedSandboxControlServer

	mu       sync.Mutex
	token    string
	sessions map[string]time.Time
}

// NewSandboxServer builds a SandboxServer reading its configuration from the
// environment at construction time.
func NewSandboxServer() *SandboxServer {
	return &SandboxServer{
		token:    os.Getenv(envSandboxControlToken),
		sessions: map[string]time.Time{},
	}
}

// Enabled reports whether control sessions can be opened.
func (s *SandboxServer) Enabled() bool { return s.token != "" }

// OpenSession validates the presented token and mints a session handle.
func (s *SandboxServer) OpenSession(_ context.Context, req *pb.OpenSessionRequest) (*pb.OpenSessionResponse, error) {
	if !s.Enabled() {
		return nil, ErrSandboxDisabled
	}
	if req.Token != s.token {
		return nil, fmt.Errorf("invalid sandbox control token")
	}

	id, err := randomSessionID()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.sessions[id] = time.Now()
	s.mu.Unlock()
	return &pb.OpenSessionResponse{SessionId: id}, nil
}

// CloseSession invalidates a session handle.
func (s *SandboxServer) CloseSession(_ context.Context, req *pb.CloseSessionRequest) (*pb.CloseSessionResponse, error) {
	s.mu.Lock()
	delete(s.sessions, req.SessionId)
	s.mu.Unlock()
	return &pb.CloseSessionResponse{}, nil
}

// Exec runs a command inside the VM within an open session.
func (s *SandboxServer) Exec(ctx context.Context, req *pb.ExecRequest) (*pb.ExecResponse, error) {
	if err := s.verifySession(req.SessionId); err != nil {
		return nil, err
	}
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, req.Command[0], req.Command[1:]...) //nolint:gosec
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := int32(0)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = intToInt32Safe(exitErr.ExitCode())
		} else {
			exitCode = 1
		}
	}
	return &pb.ExecResponse{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

func (s *SandboxServer) verifySession(id string) error {
	if !s.Enabled() {
		return ErrSandboxDisabled
	}
	s.mu.Lock()
	_, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return ErrUnknownSession
	}
	return nil
}

func randomSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
