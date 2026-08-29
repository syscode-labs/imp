package sandboxgateway

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	sandboxpb "github.com/syscode-labs/imp/internal/proto/sandbox"
)

// execTimeout caps handler-side guest work so wedged guests free the
// gateway's workers instead of accumulating them. Handlers derive their
// own context deadlines from it.
const execTimeout = 60 * time.Second

// withGuestConn dials the sandbox's VSOCK unix socket and invokes fn with a
// client bound to the guest SandboxControl service, guaranteeing Close.
// A missing socket means the VM is not on this node: callers get
// Unavailable so SDKs can re-route. The connection lifetime is NOT bounded
// here — handlers own call timeouts; grpc.NewClient dials lazily on first
// RPC under the handler's context.
func withGuestConn(socketPath string, fn func(ctx context.Context, c sandboxpb.SandboxControlClient) error) error {
	if _, err := os.Stat(socketPath); err != nil {
		return status.Errorf(codes.Unavailable, "guest socket %s not present on this node", socketPath)
	}
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := grpc.NewClient(
		"unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		cancel()
		return status.Errorf(codes.Unavailable, "dial guest %s: %v", socketPath, err)
	}
	client := sandboxpb.NewSandboxControlClient(conn)
	err = fn(ctx, client)
	cancel()
	if cerr := conn.Close(); cerr != nil && err == nil {
		err = fmt.Errorf("close guest conn: %w", cerr)
	}
	return err
}

// openGuestSession exchanges the shared guest token for a session handle.
func openGuestSession(ctx context.Context, c sandboxpb.SandboxControlClient, guestToken string) (string, error) {
	resp, err := c.OpenSession(ctx, &sandboxpb.OpenSessionRequest{Token: guestToken})
	if err != nil {
		return "", status.Errorf(codes.FailedPrecondition, "guest session: %v", err)
	}
	if resp.GetSessionId() == "" {
		return "", status.Error(codes.FailedPrecondition, "guest returned empty session")
	}
	return resp.GetSessionId(), nil
}

// closeGuestSession is best-effort; the guest expires sessions anyway.
func closeGuestSession(ctx context.Context, c sandboxpb.SandboxControlClient, sessionID string) {
	_, _ = c.CloseSession(ctx, &sandboxpb.CloseSessionRequest{SessionId: sessionID}) //nolint:errcheck
}

func (s *Server) guestToken(ctx context.Context) (string, error) {
	p, ok := principalFromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing authenticated sandbox identity")
	}
	return GuestToken(s.hmacKey, p.uid, p.namespace, p.vmName), nil
}
