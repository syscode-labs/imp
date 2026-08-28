//go:build linux

// Package guest hosts the in-VM gRPC services.
package guest

import (
	"io"
	"io/fs"
	"path/filepath"

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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

// maxFilePayload caps ReadFile/WriteFile payloads (1 MiB). Oversize reads
// return a truncated view; oversize writes are refused.
const maxFilePayload = 1 << 20

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

// safePath rejects absolute traversals outside a rooted working tree
// perspective: paths must stay inside the VM filesystem the session owns.
// The guest already has full filesystem access, so this is about refusing
// obviously malformed paths, not containment.
func safePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.Contains(p, "\x00") {
		return "", fmt.Errorf("path contains NUL")
	}
	clean := filepath.Clean("/" + p)
	if strings.HasPrefix(clean, "/proc/") || strings.HasPrefix(clean, "/sys/") ||
		clean == "/proc" || clean == "/sys" {
		return "", fmt.Errorf("path %q is not accessible", p)
	}
	return clean, nil
}

// ReadFile returns up to maxFilePayload bytes of a file.
func (s *SandboxServer) ReadFile(ctx context.Context, req *pb.ReadFileRequest) (*pb.ReadFileResponse, error) {
	if err := s.verifySession(req.SessionId); err != nil {
		return nil, err
	}
	path, err := safePath(req.Path)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_RDONLY, 0) //nolint:gosec // path cleaned above
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}

	content := make([]byte, min64(info.Size(), maxFilePayload))
	n, err := io.ReadFull(f, content)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return &pb.ReadFileResponse{
		Content:   content[:n],
		Size:      info.Size(),
		Truncated: info.Size() > maxFilePayload,
	}, nil
}

// WriteFile writes up to maxFilePayload bytes to a file, creating it.
func (s *SandboxServer) WriteFile(ctx context.Context, req *pb.WriteFileRequest) (*pb.WriteFileResponse, error) {
	if err := s.verifySession(req.SessionId); err != nil {
		return nil, err
	}
	if len(req.Content) > maxFilePayload {
		return nil, status.Error(codes.ResourceExhausted,
			fmt.Sprintf("payload %d exceeds %d byte cap", len(req.Content), maxFilePayload))
	}
	path, err := safePath(req.Path)
	if err != nil {
		return nil, err
	}

	mode := fs.FileMode(req.Mode)
	if mode == 0 {
		mode = 0o644
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if req.Append {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(path, flags, mode) //nolint:gosec // path cleaned; mode caller-chosen
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	n, err := f.Write(req.Content)
	if err != nil {
		return nil, err
	}
	return &pb.WriteFileResponse{BytesWritten: int64(n)}, nil
}

// ListDir lists one directory level.
func (s *SandboxServer) ListDir(ctx context.Context, req *pb.ListDirRequest) (*pb.ListDirResponse, error) {
	if err := s.verifySession(req.SessionId); err != nil {
		return nil, err
	}
	path, err := safePath(req.Path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]*pb.FileEntry, 0, len(entries))
	for _, e := range entries {
		entry := &pb.FileEntry{Name: e.Name(), IsDir: e.IsDir()}
		if info, err := e.Info(); err == nil {
			entry.Size = info.Size()
			entry.ModifiedUnix = info.ModTime().Unix()
		}
		out = append(out, entry)
	}
	return &pb.ListDirResponse{Entries: out}, nil
}

// Stat returns metadata for a path.
func (s *SandboxServer) Stat(ctx context.Context, req *pb.StatRequest) (*pb.StatResponse, error) {
	if err := s.verifySession(req.SessionId); err != nil {
		return nil, err
	}
	path, err := safePath(req.Path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &pb.StatResponse{
		Size:         info.Size(),
		IsDir:        info.IsDir(),
		ModifiedUnix: info.ModTime().Unix(),
		Mode:         int32(info.Mode().Perm()),
	}, nil
}

// Remove deletes a file or directory.
func (s *SandboxServer) Remove(ctx context.Context, req *pb.RemoveRequest) (*pb.RemoveResponse, error) {
	if err := s.verifySession(req.SessionId); err != nil {
		return nil, err
	}
	switch req.Path {
	case "", "/", "/proc", "/sys", "/usr", "/bin", "/sbin", "/etc", "/var", "/home":
		return nil, fmt.Errorf("refusing to remove %q", req.Path)
	}
	path, err := safePath(req.Path)
	if err != nil {
		return nil, err
	}

	if req.Recursive {
		return &pb.RemoveResponse{}, os.RemoveAll(path)
	}
	return &pb.RemoveResponse{}, os.Remove(path)
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func randomSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
