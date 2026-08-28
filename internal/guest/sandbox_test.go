//go:build linux

package guest

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/syscode-labs/imp/internal/proto/sandbox"
)

func TestSandboxServer_disabledWithoutToken(t *testing.T) {
	t.Setenv(envSandboxControlToken, "")
	s := NewSandboxServer()

	assert.False(t, s.Enabled())

	_, err := s.OpenSession(context.Background(), &pb.OpenSessionRequest{Token: "whatever"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSandboxDisabled))

	_, err = s.Exec(context.Background(), &pb.ExecRequest{SessionId: "x", Command: []string{"true"}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSandboxDisabled))
}

func TestSandboxServer_sessionLifecycle(t *testing.T) {
	t.Setenv(envSandboxControlToken, "secret-token")
	s := NewSandboxServer()
	require.True(t, s.Enabled())

	_, err := s.OpenSession(context.Background(), &pb.OpenSessionRequest{Token: "wrong"})
	require.Error(t, err)

	open, err := s.OpenSession(context.Background(), &pb.OpenSessionRequest{Token: "secret-token"})
	require.NoError(t, err)
	require.NotEmpty(t, open.SessionId)

	resp, err := s.Exec(context.Background(), &pb.ExecRequest{
		SessionId: open.SessionId,
		Command:   []string{"echo", "hi"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), resp.ExitCode)
	assert.Equal(t, "hi\n", resp.Stdout)

	// Exec with an unopened session id is refused.
	_, err = s.Exec(context.Background(), &pb.ExecRequest{SessionId: "bogus", Command: []string{"true"}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownSession))

	_, err = s.CloseSession(context.Background(), &pb.CloseSessionRequest{SessionId: open.SessionId})
	require.NoError(t, err)

	_, err = s.Exec(context.Background(), &pb.ExecRequest{SessionId: open.SessionId, Command: []string{"true"}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownSession))
}

func TestSandboxServer_tokenReadFromEnvAtConstruction(t *testing.T) {
	require.NoError(t, os.Unsetenv(envSandboxControlToken))
	assert.False(t, NewSandboxServer().Enabled())
}

func openProbeSession(t *testing.T, s *SandboxServer) string {
	t.Helper()
	open, err := s.OpenSession(context.Background(), &pb.OpenSessionRequest{Token: "secret-token"})
	require.NoError(t, err)
	return open.SessionId
}

func TestSandboxServer_fileLifecycle(t *testing.T) {
	t.Setenv(envSandboxControlToken, "secret-token")
	s := NewSandboxServer()
	session := openProbeSession(t, s)
	ctx := context.Background()

	dir := t.TempDir()
	target := dir + "/f.txt"

	w, err := s.WriteFile(ctx, &pb.WriteFileRequest{SessionId: session, Path: target, Content: []byte("hello file")})
	require.NoError(t, err)
	assert.Equal(t, int64(10), w.BytesWritten)

	r, err := s.ReadFile(ctx, &pb.ReadFileRequest{SessionId: session, Path: target})
	require.NoError(t, err)
	assert.Equal(t, "hello file", string(r.Content))
	assert.False(t, r.Truncated)

	st, err := s.Stat(ctx, &pb.StatRequest{SessionId: session, Path: target})
	require.NoError(t, err)
	assert.False(t, st.IsDir)
	assert.Equal(t, int64(10), st.Size)

	ls, err := s.ListDir(ctx, &pb.ListDirRequest{SessionId: session, Path: dir})
	require.NoError(t, err)
	require.Len(t, ls.Entries, 1)
	assert.Equal(t, "f.txt", ls.Entries[0].Name)

	_, err = s.Remove(ctx, &pb.RemoveRequest{SessionId: session, Path: target})
	require.NoError(t, err)
	_, err = s.Stat(ctx, &pb.StatRequest{SessionId: session, Path: target})
	assert.Error(t, err)
}

func TestSandboxServer_fileOpsRequireSession(t *testing.T) {
	t.Setenv(envSandboxControlToken, "secret-token")
	s := NewSandboxServer()
	ctx := context.Background()

	_, err := s.ReadFile(ctx, &pb.ReadFileRequest{SessionId: "bogus", Path: "/etc/hostname"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownSession))

	_, err = s.WriteFile(ctx, &pb.WriteFileRequest{SessionId: "bogus", Path: "/tmp/x", Content: []byte("y")})
	assert.True(t, errors.Is(err, ErrUnknownSession))
}

func TestSandboxServer_oversizeWriteRefused(t *testing.T) {
	t.Setenv(envSandboxControlToken, "secret-token")
	s := NewSandboxServer()
	session := openProbeSession(t, s)

	_, err := s.WriteFile(context.Background(), &pb.WriteFileRequest{
		SessionId: session, Path: "/tmp/big", Content: make([]byte, maxFilePayload+1),
	})
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestSandboxServer_removeRefusesRootPaths(t *testing.T) {
	t.Setenv(envSandboxControlToken, "secret-token")
	s := NewSandboxServer()
	session := openProbeSession(t, s)

	for _, p := range []string{"/", "/usr", "/etc"} {
		_, err := s.Remove(context.Background(), &pb.RemoveRequest{SessionId: session, Path: p})
		require.Error(t, err, p)
	}
}
