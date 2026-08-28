//go:build linux

package guest

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
