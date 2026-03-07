//go:build linux

// Package vsock provides a gRPC dialer over Firecracker's VSOCK Unix socket proxy.
package vsock

import (
	"context"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Dial connects to a guest VM's gRPC server via Firecracker's VSOCK Unix socket proxy.
// sockPath is the path to the Firecracker VSOCK Unix socket (e.g. /run/imp/sockets/vmname.vsock).
// port is the guest VSOCK port (e.g. 10000).
func Dial(ctx context.Context, sockPath string, port uint32) (*grpc.ClientConn, error) {
	return grpc.NewClient("passthrough:///vsock",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dialVSOCK(ctx, sockPath, port)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// dialVSOCK connects to the Firecracker VSOCK proxy Unix socket and performs
// the CONNECT handshake to reach the guest on the given port.
func dialVSOCK(ctx context.Context, sockPath string, port uint32) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("dial vsock proxy %s: %w", sockPath, err)
	}

	// Send CONNECT handshake
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("vsock CONNECT: %w", err)
	}

	// Read OK response
	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("vsock read response: %w", err)
	}
	resp := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(resp, "OK") {
		_ = conn.Close()
		return nil, fmt.Errorf("vsock handshake failed: %q", resp)
	}
	return conn, nil
}
