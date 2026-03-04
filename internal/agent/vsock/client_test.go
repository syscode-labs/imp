//go:build linux

package vsock_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"

	gvsock "github.com/syscode-labs/imp/internal/agent/vsock"
	"github.com/syscode-labs/imp/internal/guest"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
)

// startFakeProxy simulates Firecracker's VSOCK Unix socket proxy.
// It accepts "CONNECT {port}\n", responds "OK {port}\n", then forwards to a real gRPC server.
func startFakeProxy(t *testing.T, grpcAddr string) string {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "vsock.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go handleProxyConn(conn, grpcAddr)
		}
	}()
	t.Cleanup(func() { l.Close() })
	return sockPath
}

func handleProxyConn(proxy net.Conn, grpcAddr string) {
	defer proxy.Close()
	buf := make([]byte, 64)
	n, _ := proxy.Read(buf)
	line := strings.TrimSpace(string(buf[:n]))
	var port int
	fmt.Sscanf(line, "CONNECT %d", &port)
	fmt.Fprintf(proxy, "OK %d\n", port)

	backend, err := net.Dial("tcp", grpcAddr)
	if err != nil {
		return
	}
	defer backend.Close()
	go func() { io.Copy(backend, proxy) }() //nolint:errcheck
	io.Copy(proxy, backend)                  //nolint:errcheck
}

func TestVSOCKClient_callsGuestAgent(t *testing.T) {
	// Start a real gRPC server on a TCP port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	pb.RegisterGuestAgentServer(srv, guest.NewServer())
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(srv.GracefulStop)

	// Start the fake VSOCK proxy in front of it
	proxyPath := startFakeProxy(t, lis.Addr().String())

	// Use our VSOCK client
	conn, err := gvsock.Dial(context.Background(), proxyPath, 10000)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := pb.NewGuestAgentClient(conn)
	resp, err := client.Exec(context.Background(), &pb.ExecRequest{
		Command:        []string{"echo", "vsock-ok"},
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", resp.ExitCode)
	}
}
