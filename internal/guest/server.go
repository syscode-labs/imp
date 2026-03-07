//go:build linux

package guest

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"strings"
	"time"

	pb "github.com/syscode-labs/imp/internal/proto/guest"
)

// Server implements the GuestAgent gRPC service.
type Server struct {
	pb.UnimplementedGuestAgentServer
}

// NewServer returns a new Server.
func NewServer() *Server { return &Server{} }

// Exec runs a command and returns exit code, stdout, and stderr.
func (s *Server) Exec(ctx context.Context, req *pb.ExecRequest) (*pb.ExecResponse, error) {
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
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

// HTTPCheck performs an HTTP GET inside the VM against localhost:{port}{path}.
func (s *Server) HTTPCheck(_ context.Context, req *pb.HTTPCheckRequest) (*pb.HTTPCheckResponse, error) {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	path := req.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", req.Port, path)
	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return &pb.HTTPCheckResponse{StatusCode: 0}, nil
	}
	defer resp.Body.Close() //nolint:errcheck
	return &pb.HTTPCheckResponse{StatusCode: intToInt32Safe(resp.StatusCode)}, nil
}

// Metrics reads CPU, memory and disk usage from /proc and syscall.
func (s *Server) Metrics(_ context.Context, _ *pb.MetricsRequest) (*pb.MetricsResponse, error) {
	cpu, iowait, err := cpuAndIOWaitUsage()
	if err != nil {
		cpu = 0
		iowait = 0
	}
	mem, err := memUsedBytes()
	if err != nil {
		mem = 0
	}
	disk, err := diskUsedBytes("/")
	if err != nil {
		disk = 0
	}
	return &pb.MetricsResponse{
		CpuUsageRatio:   cpu,
		CpuIowaitRatio:  iowait,
		MemoryUsedBytes: mem,
		DiskUsedBytes:   disk,
	}, nil
}

func intToInt32Safe(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}
