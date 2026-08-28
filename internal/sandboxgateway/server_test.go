package sandboxgateway

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	sandboxpb "github.com/syscode-labs/imp/internal/proto/sandbox"
	gwpb "github.com/syscode-labs/imp/internal/proto/sandboxgateway"
)

const testKey = "unit-test-key"

func TestAuthorize_missingAndBadMetadata(t *testing.T) {
	s, err := NewServer(Options{SocketDir: t.TempDir(), HMACKey: []byte(testKey)})
	require.NoError(t, err)

	err = s.authorize(context.Background())
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	md := metadata.Pairs(metaSandboxUID, "uid1")
	err = s.authorize(metadata.NewIncomingContext(context.Background(), md))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	md = metadata.Join(metadata.Pairs(metaSandboxUID, "uid1"), metadata.Pairs(metaAuthorization, "Basic abc"))
	err = s.authorize(metadata.NewIncomingContext(context.Background(), md))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	md = metadata.Join(
		metadata.Pairs(metaSandboxUID, "uid1"),
		metadata.Pairs(metaAuthorization, "Bearer "+Token([]byte(testKey), "uid1")))
	assert.NoError(t, s.authorize(metadata.NewIncomingContext(context.Background(), md)))

	md = metadata.Join(
		metadata.Pairs(metaSandboxUID, "uid1"),
		metadata.Pairs(metaAuthorization, "Bearer "+Token([]byte(testKey), "uid2")))
	assert.Equal(t, codes.Unauthenticated, status.Code(s.authorize(metadata.NewIncomingContext(context.Background(), md))))
}

// fakeGuest is a minimal guest SandboxControl server for proxy tests.
type fakeGuest struct {
	sandboxpb.UnimplementedSandboxControlServer
	token   string
	gotPath string
}

func (f *fakeGuest) OpenSession(_ context.Context, r *sandboxpb.OpenSessionRequest) (*sandboxpb.OpenSessionResponse, error) {
	if r.Token != f.token {
		return nil, status.Error(codes.Unauthenticated, "bad guest token")
	}
	return &sandboxpb.OpenSessionResponse{SessionId: "sess-1"}, nil
}

func (f *fakeGuest) ReadFile(_ context.Context, r *sandboxpb.ReadFileRequest) (*sandboxpb.ReadFileResponse, error) {
	f.gotPath = r.Path
	return &sandboxpb.ReadFileResponse{Content: []byte("data"), Size: 4}, nil
}

func (f *fakeGuest) Exec(_ context.Context, r *sandboxpb.ExecRequest) (*sandboxpb.ExecResponse, error) {
	return &sandboxpb.ExecResponse{ExitCode: 3, Stdout: "out", Stderr: "err"}, nil
}

func (f *fakeGuest) WriteFile(_ context.Context, r *sandboxpb.WriteFileRequest) (*sandboxpb.WriteFileResponse, error) {
	return &sandboxpb.WriteFileResponse{BytesWritten: int64(len(r.GetContent()))}, nil
}

// startStack wires gateway→fake guest over a real unix socket pair and
// returns a client plus cleanup.
func startStack(t *testing.T, guest *fakeGuest) gwpb.SandboxGatewayClient {
	t.Helper()
	// Short base dir: macOS rejects unix sockets whose full path exceeds
	// 104 chars, and testing.T temp dirs are long.
	socketDir, err := os.MkdirTemp("/tmp", "sbxgw")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	vmSocket := VSOCKPath(socketDir, "ns1", "vm1")

	guestLis, err := net.Listen("unix", vmSocket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = guestLis.Close() })
	gs := grpc.NewServer()
	sandboxpb.RegisterSandboxControlServer(gs, guest)
	go func() { _ = gs.Serve(guestLis) }()

	srv, err := NewServer(Options{SocketDir: socketDir, HMACKey: []byte(testKey)})
	require.NoError(t, err)

	bufLis := bufconn.Listen(1 << 20)
	gw := grpc.NewServer(
		grpc.ChainUnaryInterceptor(srv.AuthUnary),
		grpc.ChainStreamInterceptor(srv.AuthStream),
	)
	gwpb.RegisterSandboxGatewayServer(gw, srv)
	go func() { _ = gw.Serve(bufLis) }()
	t.Cleanup(gw.Stop)

	gwTarget := fmt.Sprintf("passthrough:///bufnet-%p", bufLis)
	conn, err := grpc.NewClient(gwTarget,
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return bufLis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return gwpb.NewSandboxGatewayClient(conn)
}

func authed(ctx context.Context, uid string) context.Context {
	// Outgoing: client-sent metadata. Incoming-context metadata is a
	// server-side concept and never leaves the process.
	return metadata.AppendToOutgoingContext(ctx,
		metaSandboxUID, uid,
		metaAuthorization, "Bearer "+Token([]byte(testKey), uid))
}

func TestGateway_endToEndExecAndFiles(t *testing.T) {
	t.Setenv("GATEWAY_GUEST_TOKEN", "guest-secret")
	client := startStack(t, &fakeGuest{token: "guest-secret"})
	ctx := authed(context.Background(), "uid1")

	stream, err := client.Exec(ctx, &gwpb.ExecRequest{
		Vm:      &gwpb.VMRef{Namespace: "ns1", VmName: "vm1"},
		Command: []string{"echo", "hi"},
	})
	require.NoError(t, err)

	var frames []*gwpb.ExecFrame
	for {
		fr, err := stream.Recv()
		if err != nil {
			break
		}
		frames = append(frames, fr)
	}
	require.Len(t, frames, 3)
	assert.Equal(t, "out", frames[0].GetStdout())
	assert.Equal(t, "err", frames[1].GetStderr())
	assert.True(t, frames[2].GetFinal())
	assert.Equal(t, int32(3), frames[2].GetExitCode())

	w, err := client.WriteFile(ctx, &gwpb.WriteFileRequest{
		Vm: &gwpb.VMRef{Namespace: "ns1", VmName: "vm1"}, Path: "/tmp/x", Content: []byte("abc"),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), w.GetBytesWritten())
}

func TestGateway_unauthenticatedRejectedBeforeGuest(t *testing.T) {
	client := startStack(t, &fakeGuest{token: "guest-secret"})

	_, err := client.ReadFile(context.Background(), &gwpb.ReadFileRequest{
		Vm: &gwpb.VMRef{Namespace: "ns1", VmName: "vm1"}, Path: "/etc/passwd",
	})
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	// Valid-format token bound to a different uid: mismatch.
	bad := metadata.AppendToOutgoingContext(context.Background(),
		metaSandboxUID, "someone-else",
		metaAuthorization, "Bearer "+Token([]byte(testKey), "uid1"))
	_, err = client.ReadFile(bad, &gwpb.ReadFileRequest{
		Vm: &gwpb.VMRef{Namespace: "ns1", VmName: "vm1"}, Path: "/etc/passwd",
	})
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestGateway_guestSocketAbsent(t *testing.T) {
	client := startStack(t, &fakeGuest{token: "guest-secret"})
	ctx := authed(context.Background(), "uid1")

	t.Setenv("GATEWAY_GUEST_TOKEN", "guest-secret")
	_, err := client.ReadFile(ctx, &gwpb.ReadFileRequest{
		Vm: &gwpb.VMRef{Namespace: "ns1", VmName: "ghost"}, Path: "/x",
	})
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestVSOCKPath_matchesAgentConvention(t *testing.T) {
	// The agent writes <SocketDir>/<ns>-<name>.sock with .vsock sibling.
	dir := t.TempDir()
	p := VSOCKPath(dir, "team-a", "sb1")
	assert.Equal(t, filepath.Join(dir, "team-a-sb1.vsock"), p)
	_, err := os.Stat(p)
	assert.Error(t, err, fmt.Sprintf("socket %s must not pre-exist", p))
}
