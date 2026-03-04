//go:build linux

package guest_test

import (
	"context"
	"net"
	"net/http"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/syscode-labs/imp/internal/proto/guest"
	"github.com/syscode-labs/imp/internal/guest"
)

func startTestServer(t *testing.T) pb.GuestAgentClient {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	pb.RegisterGuestAgentServer(srv, guest.NewServer())
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(srv.GracefulStop)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() }) //nolint:errcheck
	return pb.NewGuestAgentClient(conn)
}

func TestExec_echo(t *testing.T) {
	client := startTestServer(t)
	resp, err := client.Exec(context.Background(), &pb.ExecRequest{
		Command:        []string{"echo", "hello"},
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", resp.ExitCode)
	}
	if resp.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", resp.Stdout, "hello\n")
	}
}

func TestExec_nonzero(t *testing.T) {
	client := startTestServer(t)
	resp, err := client.Exec(context.Background(), &pb.ExecRequest{
		Command:        []string{"false"},
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestExec_timeout(t *testing.T) {
	client := startTestServer(t)
	resp, err := client.Exec(context.Background(), &pb.ExecRequest{
		Command:        []string{"sleep", "10"},
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ExitCode == 0 {
		t.Error("expected non-zero exit code from timeout")
	}
}

func TestHTTPCheck_ok(t *testing.T) {
	httpLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { httpLis.Close() })
	port := httpLis.Addr().(*net.TCPAddr).Port
	go func() {
		http.Serve(httpLis, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:errcheck
			w.WriteHeader(200)
		}))
	}()

	client := startTestServer(t)
	resp, err := client.HTTPCheck(context.Background(), &pb.HTTPCheckRequest{
		Port:           int32(port),
		Path:           "/",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status_code = %d, want 200", resp.StatusCode)
	}
}

func TestMetrics_returns(t *testing.T) {
	client := startTestServer(t)
	resp, err := client.Metrics(context.Background(), &pb.MetricsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.CpuUsageRatio < 0 || resp.CpuUsageRatio > 1 {
		t.Errorf("cpu_usage_ratio = %f, want 0-1", resp.CpuUsageRatio)
	}
	if resp.MemoryUsedBytes < 0 {
		t.Errorf("memory_used_bytes = %d, want >= 0", resp.MemoryUsedBytes)
	}
}

func TestHTTPCheck_connectionRefused(t *testing.T) {
	// Use a port we know has no listener (bind then close to get a free port)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // close it so nothing is listening

	client := startTestServer(t)
	resp, err := client.HTTPCheck(context.Background(), &pb.HTTPCheckRequest{
		Port:           int32(port),
		Path:           "/",
		TimeoutSeconds: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 0 {
		t.Errorf("status_code = %d, want 0 for connection refused", resp.StatusCode)
	}
}
