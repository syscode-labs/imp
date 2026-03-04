# Phase 1 Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement VSOCK guest agent + probes, Prometheus metrics (operator + node agent), and Layer 1 E2E tests — completing Phase 1 of the Imp operator.

**Architecture:** A `guest-agent` binary is injected into VM rootfs at build time (added to the OCI extraction tmpDir before `mke2fs`, cached under `{digest}-ga`). At boot, `init=/.imp/init` is appended to kernel_args; the init wrapper starts the guest agent then exec-chains `/sbin/init`. The guest agent is a gRPC server on VSOCK port 10000; the node agent connects via Firecracker's Unix socket proxy (`{SocketDir}/{vmName}.vsock`). A probe-runner goroutine polls probes after the VM reaches Running and patches `ImpVM.status.conditions`. Both operator (:8080) and agent (:9090) expose Prometheus `/metrics`; a `ServiceMonitor` and `PodMonitor` in the Helm chart enable Prometheus Operator auto-discovery. Layer 1 E2E tests run in Kind against a deployed Helm chart.

**Tech Stack:** Go, gRPC + protobuf (buf for codegen), `github.com/mdlayher/vsock` (guest VSOCK listener), `github.com/prometheus/client_golang` (metrics — already indirect), Kind + Helm (E2E).

**Worktree:** `~/.config/superpowers/worktrees/imp/phase1-completion`

**Run tests with:**
```bash
KUBEBUILDER_ASSETS="$(/Users/giovanni/syscode/git/imp/bin/setup-envtest use --bin-dir /Users/giovanni/syscode/git/imp/bin/k8s -p path 2>/dev/null)" go test ./...
```

---

### Task 1: gRPC dependencies + proto definition + code generation

**Files:**
- Create: `internal/proto/guest/guest.proto`
- Create: `internal/proto/guest/guest.pb.go` (generated)
- Create: `internal/proto/guest/guest_grpc.pb.go` (generated)
- Create: `buf.yaml`
- Create: `buf.gen.yaml`

**Step 1: Add gRPC dependencies**

```bash
cd ~/.config/superpowers/worktrees/imp/phase1-completion
go get google.golang.org/grpc@latest
go get github.com/mdlayher/vsock@latest
go mod tidy
```

Expected: `go.mod` gains `google.golang.org/grpc` and `github.com/mdlayher/vsock` as direct deps.

**Step 2: Install buf and protoc plugins**

```bash
brew install bufbuild/buf/buf
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

**Step 3: Create `buf.yaml`**

```yaml
version: v2
modules:
  - path: internal/proto
```

**Step 4: Create `buf.gen.yaml`**

```yaml
version: v2
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: github.com/syscode-labs/imp
plugins:
  - local: protoc-gen-go
    out: .
    opt: paths=source_relative
  - local: protoc-gen-go-grpc
    out: .
    opt: paths=source_relative
```

**Step 5: Create `internal/proto/guest/guest.proto`**

```protobuf
syntax = "proto3";

package guest;

option go_package = "github.com/syscode-labs/imp/internal/proto/guest";

service GuestAgent {
  rpc Exec(ExecRequest) returns (ExecResponse);
  rpc HTTPCheck(HTTPCheckRequest) returns (HTTPCheckResponse);
  rpc Metrics(MetricsRequest) returns (MetricsResponse);
}

message ExecRequest {
  repeated string command = 1;
  int32 timeout_seconds = 2;
}

message ExecResponse {
  int32 exit_code = 1;
  string stdout = 2;
  string stderr = 3;
}

message HTTPCheckRequest {
  int32 port = 1;
  string path = 2;
  map<string, string> headers = 3;
  int32 timeout_seconds = 4;
}

message HTTPCheckResponse {
  int32 status_code = 1;
}

message MetricsRequest {}

message MetricsResponse {
  double cpu_usage_ratio   = 1;
  int64  memory_used_bytes = 2;
  int64  disk_used_bytes   = 3;
}
```

**Step 6: Generate Go code**

```bash
buf generate
```

Expected: `internal/proto/guest/guest.pb.go` and `internal/proto/guest/guest_grpc.pb.go` created.

**Step 7: Verify it compiles**

```bash
go build ./internal/proto/...
```

Expected: no errors.

**Step 8: Commit**

```bash
git add buf.yaml buf.gen.yaml internal/proto/ go.mod go.sum
git commit -m "feat(proto): gRPC guest agent service definition"
```

---

### Task 2: Guest agent server (Exec, HTTPCheck, Metrics)

**Files:**
- Create: `internal/guest/server.go`
- Create: `internal/guest/server_test.go`

**Step 1: Write the failing tests** (`internal/guest/server_test.go`)

```go
//go:build linux

package guest_test

import (
	"context"
	"net"
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
	// Start a simple HTTP server for the check
	httpLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := httpLis.Addr().(*net.TCPAddr).Port
	go func() {
		http.Serve(httpLis, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
```

**Step 2: Run tests, verify they fail**

```bash
go test ./internal/guest/... 2>&1 | head -5
```

Expected: FAIL (package doesn't exist yet).

**Step 3: Create `internal/guest/server.go`**

```go
//go:build linux

package guest

import (
	"bytes"
	"context"
	"fmt"
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
func (s *Server) Exec(_ context.Context, req *pb.ExecRequest) (*pb.ExecResponse, error) {
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, req.Command[0], req.Command[1:]...) //nolint:gosec
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := int32(0)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
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
		return &pb.HTTPCheckResponse{StatusCode: 0}, nil // connection refused → not ready
	}
	defer resp.Body.Close() //nolint:errcheck
	return &pb.HTTPCheckResponse{StatusCode: int32(resp.StatusCode)}, nil
}

// Metrics reads CPU, memory and disk usage from /proc and df.
func (s *Server) Metrics(_ context.Context, _ *pb.MetricsRequest) (*pb.MetricsResponse, error) {
	cpu, err := cpuUsage()
	if err != nil {
		cpu = 0
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
		MemoryUsedBytes: mem,
		DiskUsedBytes:   disk,
	}, nil
}
```

**Step 4: Create `internal/guest/metrics_linux.go`** (CPU/mem/disk readers)

```go
//go:build linux

package guest

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// cpuUsage samples /proc/stat twice 100ms apart and returns the ratio 0.0–1.0.
func cpuUsage() (float64, error) {
	s1, err := readCPUStat()
	if err != nil {
		return 0, err
	}
	time.Sleep(100 * time.Millisecond)
	s2, err := readCPUStat()
	if err != nil {
		return 0, err
	}
	total := float64(s2.total - s1.total)
	idle := float64(s2.idle - s1.idle)
	if total == 0 {
		return 0, nil
	}
	return (total - idle) / total, nil
}

type cpuStat struct{ total, idle uint64 }

func readCPUStat() (cpuStat, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuStat{}, err
	}
	defer f.Close() //nolint:errcheck
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var vals []uint64
		for _, f := range fields {
			v, _ := strconv.ParseUint(f, 10, 64)
			vals = append(vals, v)
		}
		var total uint64
		for _, v := range vals {
			total += v
		}
		idle := uint64(0)
		if len(vals) > 3 {
			idle = vals[3]
		}
		return cpuStat{total: total, idle: idle}, nil
	}
	return cpuStat{}, fmt.Errorf("cpu line not found in /proc/stat")
}

// memUsedBytes returns MemTotal - MemAvailable from /proc/meminfo.
func memUsedBytes() (int64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck
	vals := map[string]int64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		v, _ := strconv.ParseInt(fields[1], 10, 64)
		vals[key] = v * 1024 // kB → bytes
	}
	return vals["MemTotal"] - vals["MemAvailable"], nil
}

// diskUsedBytes returns used bytes on the filesystem containing path.
func diskUsedBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	total := int64(stat.Blocks) * stat.Bsize
	free := int64(stat.Bfree) * stat.Bsize
	return total - free, nil
}
```

**Step 5: Run tests**

```bash
GOOS=linux go test ./internal/guest/... -v 2>&1 | tail -20
```

Expected: all 5 tests PASS (TestExec_echo, TestExec_nonzero, TestExec_timeout, TestHTTPCheck_ok, TestMetrics_returns).

Note: these tests require Linux (the `//go:build linux` tag). On macOS, run in a Docker container or via `GOOS=linux go test` — but since the CI runs on Linux, this is fine.

**Step 6: Commit**

```bash
git add internal/guest/
git commit -m "feat(guest): gRPC server — Exec, HTTPCheck, Metrics"
```

---

### Task 3: Guest agent binary + rootfs injection

**Files:**
- Create: `cmd/guest-agent/main.go`
- Modify: `internal/agent/rootfs/builder.go`
- Create: `internal/agent/rootfs/inject_test.go`

**Step 1: Write the failing injection test** (`internal/agent/rootfs/inject_test.go`)

```go
package rootfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syscode-labs/imp/internal/agent/rootfs"
)

func TestBuildOption_WithGuestAgent(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("rootfs injection requires root (loop mount)")
	}
	// Write a fake guest agent binary
	agentBin, err := os.CreateTemp("", "fake-agent-*")
	if err != nil {
		t.Fatal(err)
	}
	agentBin.WriteString("#!/bin/sh\necho guest-agent")
	agentBin.Chmod(0755)
	agentBin.Close()
	defer os.Remove(agentBin.Name())

	// Build a minimal ext4 for testing
	tmpDir := t.TempDir()
	ext4Path := filepath.Join(tmpDir, "test.ext4")
	if err := rootfs.BuildMinimalExt4ForTest(tmpDir, ext4Path, 32); err != nil {
		t.Fatal(err)
	}

	// Inject
	if err := rootfs.InjectGuestAgent(agentBin.Name(), ext4Path); err != nil {
		t.Fatal(err)
	}

	// Mount and verify files exist
	mnt := t.TempDir()
	if err := rootfs.MountExt4(ext4Path, mnt); err != nil {
		t.Fatal(err)
	}
	defer rootfs.UmountExt4(mnt) //nolint:errcheck

	if _, err := os.Stat(filepath.Join(mnt, ".imp", "guest-agent")); err != nil {
		t.Errorf(".imp/guest-agent not found after injection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mnt, ".imp", "init")); err != nil {
		t.Errorf(".imp/init not found after injection: %v", err)
	}
}
```

**Step 2: Run test, verify it fails**

```bash
sudo go test ./internal/agent/rootfs/... -run TestBuildOption_WithGuestAgent -v 2>&1 | head -5
```

Expected: FAIL (functions not defined).

**Step 3: Add injection functions to `internal/agent/rootfs/`**

Create `internal/agent/rootfs/inject.go`:

```go
//go:build linux

package rootfs

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	// GuestAgentContainerPath is where the guest agent binary lives inside the imp-agent container.
	GuestAgentContainerPath = "/opt/imp/guest-agent"

	// initScript is written as /.imp/init inside the VM rootfs.
	initScript = "#!/bin/sh\n/.imp/guest-agent &\nexec /sbin/init \"$@\"\n"
)

// InjectGuestAgent copies guestAgentSrc binary and the init wrapper into the
// ext4 image at ext4Path. The VM can then be booted with init=/.imp/init.
// Requires root (uses loop mount).
func InjectGuestAgent(guestAgentSrc, ext4Path string) error {
	mnt, err := os.MkdirTemp("", "imp-inject-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mnt) //nolint:errcheck

	if err := MountExt4(ext4Path, mnt); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	defer UmountExt4(mnt) //nolint:errcheck

	impDir := filepath.Join(mnt, ".imp")
	if err := os.MkdirAll(impDir, 0o755); err != nil {
		return err
	}

	// Copy guest agent binary
	dst := filepath.Join(impDir, "guest-agent")
	if err := copyFile(guestAgentSrc, dst, 0o755); err != nil {
		return fmt.Errorf("copy guest-agent: %w", err)
	}

	// Write init wrapper
	initPath := filepath.Join(impDir, "init")
	if err := os.WriteFile(initPath, []byte(initScript), 0o755); err != nil {
		return fmt.Errorf("write init: %w", err)
	}
	return nil
}

// MountExt4 mounts ext4Path at mnt using a loop device.
func MountExt4(ext4Path, mnt string) error {
	out, err := exec.Command("mount", "-o", "loop", ext4Path, mnt).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mount: %w\n%s", err, out)
	}
	return nil
}

// UmountExt4 unmounts mnt.
func UmountExt4(mnt string) error {
	out, err := exec.Command("umount", mnt).CombinedOutput()
	if err != nil {
		return fmt.Errorf("umount: %w\n%s", err, out)
	}
	return nil
}

// BuildMinimalExt4ForTest creates a tiny ext4 image for use in tests.
func BuildMinimalExt4ForTest(dir, dest string, sizeMiB int64) error {
	return buildExt4(context.Background(), dir, dest, sizeMiB)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close() //nolint:errcheck
	_, err = io.Copy(out, in)
	return err
}
```

**Step 4: Modify `Builder.Build()` to accept a `WithGuestAgent` option**

In `internal/agent/rootfs/builder.go`, change the `Build` signature and add a `BuildOption` type:

```go
// BuildOption is applied to the extracted rootfs directory before building ext4.
type BuildOption func(tmpDir string) error

// WithGuestAgent injects the guest agent binary and init wrapper into the rootfs.
// guestAgentSrc is the host path to the guest agent binary.
func WithGuestAgent(guestAgentSrc string) BuildOption {
	return func(tmpDir string) error {
		impDir := filepath.Join(tmpDir, ".imp")
		if err := os.MkdirAll(impDir, 0o755); err != nil {
			return err
		}
		if err := copyFile(guestAgentSrc, filepath.Join(impDir, "guest-agent"), 0o755); err != nil {
			return fmt.Errorf("inject guest-agent: %w", err)
		}
		if err := os.WriteFile(filepath.Join(impDir, "init"), []byte(initScript), 0o755); err != nil {
			return fmt.Errorf("inject init: %w", err)
		}
		return nil
	}
}

// Build returns the path to a ready-to-use ext4 image for imageRef.
// opts are applied to the extracted layer directory before building the ext4.
// When opts are provided, the cache key includes a "-ga" suffix (guest-agent variant).
func (b *Builder) Build(ctx context.Context, imageRef string, opts ...BuildOption) (string, error) {
```

Inside `Build()`, change the `dest` calculation:
```go
cacheKey := digest.Hex
if len(opts) > 0 {
    cacheKey += "-ga"
}
dest := b.cachePath(cacheKey)
```

And after `extractLayers(img, tmpDir)`, apply opts:
```go
for _, opt := range opts {
    if err := opt(tmpDir); err != nil {
        return "", fmt.Errorf("build option: %w", err)
    }
}
```

**Step 5: Create `cmd/guest-agent/main.go`**

```go
//go:build linux

// cmd/guest-agent is the gRPC server that runs inside Imp microVMs.
// It listens on VSOCK port 10000 and implements probe execution and metrics.
package main

import (
	"fmt"
	"os"

	"github.com/mdlayher/vsock"
	"google.golang.org/grpc"

	"github.com/syscode-labs/imp/internal/guest"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
)

const vsockPort = 10000

func main() {
	l, err := vsock.Listen(vsockPort, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "guest-agent: listen vsock:%d: %v\n", vsockPort, err)
		os.Exit(1)
	}
	fmt.Printf("guest-agent: listening on vsock port %d\n", vsockPort)

	srv := grpc.NewServer()
	pb.RegisterGuestAgentServer(srv, guest.NewServer())
	if err := srv.Serve(l); err != nil {
		fmt.Fprintf(os.Stderr, "guest-agent: serve: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 6: Run all tests**

```bash
KUBEBUILDER_ASSETS="..." go test ./internal/agent/rootfs/... ./internal/guest/...
```

Expected: rootfs tests pass (injection test skipped on non-root), guest tests pass.

**Step 7: Commit**

```bash
git add cmd/guest-agent/ internal/agent/rootfs/inject.go internal/agent/rootfs/builder.go internal/agent/rootfs/inject_test.go
git commit -m "feat(rootfs): guest agent injection + WithGuestAgent BuildOption"
```

---

### Task 4: GuestAgentConfig API field + resolver

**Files:**
- Modify: `api/v1alpha1/shared_types.go`
- Modify: `api/v1alpha1/impvmclass_types.go`
- Modify: `api/v1alpha1/impvmtemplate_types.go`
- Modify: `api/v1alpha1/impvm_types.go`
- Create: `internal/agent/guestagent.go`
- Create: `internal/agent/guestagent_test.go`

**Step 1: Write the failing resolver test** (`internal/agent/guestagent_test.go`)

```go
package agent_test

import (
	"testing"

	"github.com/syscode-labs/imp/internal/agent"
	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func boolPtr(b bool) *bool { return &b }

func TestResolveGuestAgentEnabled(t *testing.T) {
	tests := []struct {
		name      string
		vm        *impv1alpha1.ImpVM
		class     *impv1alpha1.ImpVMClass
		want      bool
	}{
		{
			name:  "default true when all nil",
			vm:    &impv1alpha1.ImpVM{},
			class: &impv1alpha1.ImpVMClass{},
			want:  true,
		},
		{
			name: "class disables",
			vm:   &impv1alpha1.ImpVM{},
			class: &impv1alpha1.ImpVMClass{
				Spec: impv1alpha1.ImpVMClassSpec{
					GuestAgent: &impv1alpha1.GuestAgentConfig{Enabled: boolPtr(false)},
				},
			},
			want: false,
		},
		{
			name: "vm overrides class to enable",
			vm: &impv1alpha1.ImpVM{
				Spec: impv1alpha1.ImpVMSpec{
					GuestAgent: &impv1alpha1.GuestAgentConfig{Enabled: boolPtr(true)},
				},
			},
			class: &impv1alpha1.ImpVMClass{
				Spec: impv1alpha1.ImpVMClassSpec{
					GuestAgent: &impv1alpha1.GuestAgentConfig{Enabled: boolPtr(false)},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agent.ResolveGuestAgentEnabled(tt.vm, tt.class)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test, verify it fails**

```bash
go test ./internal/agent/... -run TestResolveGuestAgentEnabled 2>&1 | head -5
```

Expected: FAIL (types and function don't exist).

**Step 3: Add `GuestAgentConfig` to `api/v1alpha1/shared_types.go`**

```go
// GuestAgentConfig controls guest agent injection into the VM rootfs.
// When enabled (default), the node agent injects /.imp/guest-agent at boot,
// enabling probes, env injection, and VM-side metrics.
// Set enabled: false for bare VMs that prioritise fast boot over observability.
type GuestAgentConfig struct {
	// Enabled controls guest agent injection. Defaults to true when omitted.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}
```

**Step 4: Add `GuestAgent` field to `ImpVMClassSpec`, `ImpVMTemplateSpec`, and `ImpVMSpec`**

In each spec struct, add:
```go
// GuestAgent controls guest agent injection. Overrides ImpVMClass when set.
// +optional
GuestAgent *GuestAgentConfig `json:"guestAgent,omitempty"`
```

**Step 5: Create `internal/agent/guestagent.go`**

```go
package agent

import impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"

// ResolveGuestAgentEnabled resolves whether the guest agent should be injected
// for the given VM. Resolution order: ImpVM → ImpVMClass → default (true).
func ResolveGuestAgentEnabled(vm *impv1alpha1.ImpVM, class *impv1alpha1.ImpVMClass) bool {
	if vm.Spec.GuestAgent != nil && vm.Spec.GuestAgent.Enabled != nil {
		return *vm.Spec.GuestAgent.Enabled
	}
	if class != nil && class.Spec.GuestAgent != nil && class.Spec.GuestAgent.Enabled != nil {
		return *class.Spec.GuestAgent.Enabled
	}
	return true // default: enabled
}
```

**Step 6: Run tests**

```bash
go test ./internal/agent/... -run TestResolveGuestAgentEnabled -v
```

Expected: all 3 cases PASS.

**Step 7: Run all tests to check no regressions**

```bash
KUBEBUILDER_ASSETS="..." go test ./...
```

**Step 8: Commit**

```bash
git add api/v1alpha1/ internal/agent/guestagent.go internal/agent/guestagent_test.go
git commit -m "feat(api): GuestAgentConfig field + resolver (ImpVMClass → ImpVM inheritance)"
```

---

### Task 5: VSOCK client (host-side gRPC dialer)

**Files:**
- Create: `internal/agent/vsock/client.go`
- Create: `internal/agent/vsock/client_test.go`

**Step 1: Write the failing test** (`internal/agent/vsock/client_test.go`)

```go
//go:build linux

package vsock_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	gvsock "github.com/syscode-labs/imp/internal/agent/vsock"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
	"github.com/syscode-labs/imp/internal/guest"
)

// fakeVsockProxy simulates Firecracker's VSOCK Unix socket proxy.
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
```

**Step 2: Run test, verify it fails**

```bash
go test ./internal/agent/vsock/... 2>&1 | head -5
```

Expected: FAIL (package doesn't exist).

**Step 3: Create `internal/agent/vsock/client.go`**

```go
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
	return grpc.NewClient("vsock",
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
		conn.Close()
		return nil, fmt.Errorf("vsock CONNECT: %w", err)
	}

	// Read OK response
	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock read response: %w", err)
	}
	resp := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(resp, "OK") {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake failed: %q", resp)
	}
	return conn, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/agent/vsock/... -v
```

Expected: TestVSOCKClient_callsGuestAgent PASS.

**Step 5: Run all tests**

```bash
KUBEBUILDER_ASSETS="..." go test ./...
```

**Step 6: Commit**

```bash
git add internal/agent/vsock/
git commit -m "feat(vsock): host-side gRPC dialer over Firecracker VSOCK proxy"
```

---

### Task 6: Probe runner

**Files:**
- Create: `internal/agent/probe/runner.go`
- Create: `internal/agent/probe/runner_test.go`

**Step 1: Write the failing tests** (`internal/agent/probe/runner_test.go`)

```go
//go:build linux

package probe_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/probe"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
)

// fakeGuestAgent is a stub gRPC server for probe tests.
type fakeGuestAgent struct {
	pb.UnimplementedGuestAgentServer
	execExitCode int32
	httpStatus   int32
}

func (f *fakeGuestAgent) Exec(_ context.Context, _ *pb.ExecRequest) (*pb.ExecResponse, error) {
	return &pb.ExecResponse{ExitCode: f.execExitCode}, nil
}
func (f *fakeGuestAgent) HTTPCheck(_ context.Context, _ *pb.HTTPCheckRequest) (*pb.HTTPCheckResponse, error) {
	return &pb.HTTPCheckResponse{StatusCode: f.httpStatus}, nil
}

func startFakeAgent(t *testing.T, agent *fakeGuestAgent) pb.GuestAgentClient {
	t.Helper()
	lis, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := grpc.NewServer()
	pb.RegisterGuestAgentServer(srv, agent)
	go srv.Serve(lis)
	t.Cleanup(srv.GracefulStop)
	conn, _ := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	t.Cleanup(func() { conn.Close() })
	return pb.NewGuestAgentClient(conn)
}

func TestRunner_readinessProbe_pass(t *testing.T) {
	client := startFakeAgent(t, &fakeGuestAgent{execExitCode: 0})
	probeSpec := &impv1alpha1.ProbeSpec{
		ReadinessProbe: &impv1alpha1.Probe{
			Exec:                &impv1alpha1.ExecAction{Command: []string{"true"}},
			PeriodSeconds:       1,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
	}
	conditions := make(chan []impv1alpha1.Condition, 1)
	r := probe.NewRunner(client, probeSpec, func(conds []impv1alpha1.Condition) {
		conditions <- conds
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go r.Run(ctx)

	select {
	case conds := <-conditions:
		found := false
		for _, c := range conds {
			if c.Type == "Ready" && c.Status == "True" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected Ready=True condition, got %v", conds)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for probe condition")
	}
}

func TestRunner_readinessProbe_fail(t *testing.T) {
	client := startFakeAgent(t, &fakeGuestAgent{execExitCode: 1})
	probeSpec := &impv1alpha1.ProbeSpec{
		ReadinessProbe: &impv1alpha1.Probe{
			Exec:             &impv1alpha1.ExecAction{Command: []string{"false"}},
			PeriodSeconds:    1,
			FailureThreshold: 1,
		},
	}
	conditions := make(chan []impv1alpha1.Condition, 1)
	r := probe.NewRunner(client, probeSpec, func(conds []impv1alpha1.Condition) {
		conditions <- conds
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go r.Run(ctx)

	select {
	case conds := <-conditions:
		found := false
		for _, c := range conds {
			if c.Type == "Ready" && c.Status == "False" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected Ready=False condition, got %v", conds)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for probe condition")
	}
}
```

**Step 2: Run tests, verify they fail**

```bash
go test ./internal/agent/probe/... 2>&1 | head -5
```

Expected: FAIL.

**Step 3: Create `internal/agent/probe/runner.go`**

```go
//go:build linux

// Package probe polls VM health probes via the VSOCK guest agent.
package probe

import (
	"context"
	"time"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionPatcher is called when probe results produce updated conditions.
type ConditionPatcher func(conditions []impv1alpha1.Condition)

// Runner polls probes on a schedule and calls patcher with updated conditions.
type Runner struct {
	client  pb.GuestAgentClient
	spec    *impv1alpha1.ProbeSpec
	patcher ConditionPatcher
}

// NewRunner creates a Runner that polls probeSpec via client and calls patcher on changes.
func NewRunner(client pb.GuestAgentClient, spec *impv1alpha1.ProbeSpec, patcher ConditionPatcher) *Runner {
	return &Runner{client: client, spec: spec, patcher: patcher}
}

// Run starts polling probes until ctx is cancelled. Call in a goroutine.
func (r *Runner) Run(ctx context.Context) {
	if r.spec == nil {
		return
	}
	// Poll each probe type concurrently.
	if r.spec.ReadinessProbe != nil {
		go r.pollProbe(ctx, "Ready", r.spec.ReadinessProbe)
	}
	if r.spec.LivenessProbe != nil {
		go r.pollProbe(ctx, "Healthy", r.spec.LivenessProbe)
	}
	<-ctx.Done()
}

func (r *Runner) pollProbe(ctx context.Context, condType string, p *impv1alpha1.Probe) {
	period := time.Duration(p.PeriodSeconds) * time.Second
	if period <= 0 {
		period = 10 * time.Second
	}
	failureThreshold := p.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	successThreshold := p.SuccessThreshold
	if successThreshold <= 0 {
		successThreshold = 1
	}

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	var consecutiveFails, consecutiveSuccesses int32

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok := r.runProbe(ctx, p)
			if ok {
				consecutiveFails = 0
				consecutiveSuccesses++
				if consecutiveSuccesses >= successThreshold {
					r.patcher([]impv1alpha1.Condition{readyCondition(condType, true)})
				}
			} else {
				consecutiveSuccesses = 0
				consecutiveFails++
				if consecutiveFails >= failureThreshold {
					r.patcher([]impv1alpha1.Condition{readyCondition(condType, false)})
				}
			}
		}
	}
}

func (r *Runner) runProbe(ctx context.Context, p *impv1alpha1.Probe) bool {
	timeout := time.Duration(p.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if p.Exec != nil {
		resp, err := r.client.Exec(ctx, &pb.ExecRequest{
			Command:        p.Exec.Command,
			TimeoutSeconds: int32(timeout.Seconds()),
		})
		return err == nil && resp.ExitCode == 0
	}
	if p.HTTP != nil {
		resp, err := r.client.HTTPCheck(ctx, &pb.HTTPCheckRequest{
			Port:           p.HTTP.Port,
			Path:           p.HTTP.Path,
			TimeoutSeconds: int32(timeout.Seconds()),
		})
		return err == nil && resp.StatusCode >= 200 && resp.StatusCode < 400
	}
	return false
}

func readyCondition(condType string, ready bool) impv1alpha1.Condition {
	status := "True"
	reason := "ProbeSucceeded"
	msg := "probe passed"
	if !ready {
		status = "False"
		reason = "ProbeFailed"
		msg = "probe failed"
	}
	return impv1alpha1.Condition{
		Type:               condType,
		Status:             metav1.ConditionStatus(status),
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	}
}
```

**Step 4: Run tests**

```bash
go test ./internal/agent/probe/... -v
```

Expected: TestRunner_readinessProbe_pass and TestRunner_readinessProbe_fail PASS.

**Step 5: Run all tests**

```bash
KUBEBUILDER_ASSETS="..." go test ./...
```

**Step 6: Commit**

```bash
git add internal/agent/probe/
git commit -m "feat(probe): probe runner — polls gRPC exec/http probes, patches conditions"
```

---

### Task 7: Wire guest agent into FirecrackerDriver

**Files:**
- Modify: `internal/agent/firecracker_driver.go`
- Modify: `internal/agent/firecracker_driver_test.go`

**Step 1: Write the failing test**

In `internal/agent/firecracker_driver_test.go`, add a test that verifies `Start()` calls injection when `guestAgent.enabled` is true in the class. Look at existing driver tests for the pattern — they use the stub driver, so add a unit test for the `guestAgentEnabled` resolution only (the full integration test requires KVM).

Add to the existing test file:
```go
func TestFirecrackerDriver_resolveGuestAgent(t *testing.T) {
	d := &FirecrackerDriver{GuestAgentPath: "/opt/imp/guest-agent"}
	vm := &impv1alpha1.ImpVM{}
	class := &impv1alpha1.ImpVMClass{}
	if !d.guestAgentEnabled(vm, class) {
		t.Error("expected guest agent enabled by default")
	}
	class.Spec.GuestAgent = &impv1alpha1.GuestAgentConfig{Enabled: boolPtr(false)}
	if d.guestAgentEnabled(vm, class) {
		t.Error("expected guest agent disabled when class sets enabled=false")
	}
}
```

**Step 2: Modify `FirecrackerDriver`**

Add fields to the struct:
```go
// GuestAgentPath is the host path to the guest-agent binary for VM injection.
// Defaults to /opt/imp/guest-agent when empty.
GuestAgentPath string
```

Add a `guestAgentEnabled` helper method:
```go
func (d *FirecrackerDriver) guestAgentEnabled(vm *impv1alpha1.ImpVM, class *impv1alpha1.ImpVMClass) bool {
    return ResolveGuestAgentEnabled(vm, class)
}

func (d *FirecrackerDriver) guestAgentPath() string {
    if d.GuestAgentPath != "" {
        return d.GuestAgentPath
    }
    return rootfs.GuestAgentContainerPath
}
```

**Step 3: Modify `Start()` to inject guest agent and configure VSOCK device**

In the `Start()` method, after fetching the class (step 1) and before building the rootfs (step 2):

```go
// Determine guest agent injection.
gaEnabled := d.guestAgentEnabled(vm, &class)

// Build ext4 rootfs — with guest agent injection if enabled.
var buildOpts []rootfs.BuildOption
if gaEnabled {
    buildOpts = append(buildOpts, rootfs.WithGuestAgent(d.guestAgentPath()))
}
rootfsPath, err := d.Cache.Build(ctx, vm.Spec.Image, buildOpts...)
```

In `buildConfig()`, add VSOCK device and append `init=/.imp/init` to kernel_args when `gaEnabled` is passed in. Change `buildConfig` signature to accept `gaEnabled bool`:

```go
func (d *FirecrackerDriver) buildConfig(class *impv1alpha1.ImpVMClassSpec, rootfsPath, sockPath string, netInfo *network.NetworkInfo, gaEnabled bool) firecracker.Config {
```

Inside `buildConfig`, when `gaEnabled`:
```go
kernelArgs := d.KernelArgs
if gaEnabled {
    kernelArgs += " init=/.imp/init"
}
```

Add VSOCK device to the Firecracker config:
```go
vsockPath := strings.TrimSuffix(sockPath, ".sock") + ".vsock"
// ... in cfg:
VsockDevices: []firecracker.VsockDevice{
    {Path: vsockPath, CID: 3},
},
```

**Step 4: Start probe runner after VM reaches Running**

After `m.Start(ctx)` succeeds, store the vsock path and schedule probe runner:

```go
// Start probe runner if guest agent is enabled and probes are configured.
if gaEnabled {
    vsockPath := strings.TrimSuffix(sockPath, ".sock") + ".vsock"
    go d.runProbes(ctx, vm, vsockPath)
}
```

Add `runProbes` method:
```go
func (d *FirecrackerDriver) runProbes(ctx context.Context, vm *impv1alpha1.ImpVM, vsockPath string) {
    probeSpec := resolveProbeSpec(vm) // resolve from class/template/vm
    if probeSpec == nil {
        return
    }
    conn, err := agentvsock.Dial(ctx, vsockPath, 10000)
    if err != nil {
        // log and return — probes are best-effort
        return
    }
    defer conn.Close()
    client := pb.NewGuestAgentClient(conn)
    runner := probe.NewRunner(client, probeSpec, func(conds []impv1alpha1.Condition) {
        // Patch ImpVM.status.conditions via d.Client
        // (best-effort: log errors, don't crash)
    })
    runner.Run(ctx)
}
```

**Step 5: Run tests**

```bash
KUBEBUILDER_ASSETS="..." go test ./internal/agent/... -v
```

Expected: all tests pass including the new `TestFirecrackerDriver_resolveGuestAgent`.

**Step 6: Commit**

```bash
git add internal/agent/firecracker_driver.go internal/agent/firecracker_driver_test.go
git commit -m "feat(agent): wire guest agent injection + probe runner into FirecrackerDriver"
```

---

### Task 8: Operator Prometheus metrics

**Files:**
- Modify: `api/v1alpha1/impvm_types.go` (add timestamp fields to status)
- Create: `internal/controller/metrics.go`
- Modify: `internal/controller/impvm_controller.go` (record timestamps + observe histograms)
- Create: `internal/controller/metrics_test.go`

**Step 1: Write the failing metrics test** (`internal/controller/metrics_test.go`)

```go
package controller_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/syscode-labs/imp/internal/controller"
)

func TestSchedulingLatency_observed(t *testing.T) {
	controller.ResetMetricsForTest()
	controller.ObserveSchedulingLatency(500 * time.Millisecond)

	count := testutil.ToFloat64(controller.SchedulingLatencyHistogram)
	if count == 0 {
		t.Error("expected scheduling latency to be observed")
	}
}

func TestBootLatency_observed(t *testing.T) {
	controller.ResetMetricsForTest()
	controller.ObserveBootLatency(2 * time.Second)

	count := testutil.ToFloat64(controller.BootLatencyHistogram)
	if count == 0 {
		t.Error("expected boot latency to be observed")
	}
}
```

**Step 2: Run test, verify it fails**

```bash
go test ./internal/controller/... -run TestSchedulingLatency -v 2>&1 | head -5
```

**Step 3: Create `internal/controller/metrics.go`**

```go
package controller

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SchedulingLatencyHistogram measures time from ImpVM creation to node assignment.
	SchedulingLatencyHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "imp_vm_scheduling_latency_seconds",
		Help:    "Time from ImpVM creation to node assignment (Pending→Scheduled).",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
	})

	// BootLatencyHistogram measures time from node assignment to VM Running.
	BootLatencyHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "imp_vm_boot_latency_seconds",
		Help:    "Time from ImpVM node assignment to Running state (Scheduled→Running).",
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 8),
	})
)

// ObserveSchedulingLatency records a scheduling latency observation.
func ObserveSchedulingLatency(d time.Duration) {
	SchedulingLatencyHistogram.Observe(d.Seconds())
}

// ObserveBootLatency records a boot latency observation.
func ObserveBootLatency(d time.Duration) {
	BootLatencyHistogram.Observe(d.Seconds())
}

// ResetMetricsForTest resets histograms to zero. Only call in tests.
func ResetMetricsForTest() {
	// promauto histograms can't be reset directly; unregister and re-register.
	// For testing, use prometheus.NewHistogram + manual registration instead.
	// This is left as a no-op — testutil.ToFloat64 reads the current value.
}
```

**Step 4: Add `ScheduledAt` and `RunningAt` timestamp fields to `ImpVMStatus`**

In `api/v1alpha1/impvm_types.go`, add to `ImpVMStatus`:
```go
// ScheduledAt is the time the VM was assigned to a node.
// +optional
ScheduledAt *metav1.Time `json:"scheduledAt,omitempty"`

// RunningAt is the time the VM first reached Running phase.
// +optional
RunningAt *metav1.Time `json:"runningAt,omitempty"`
```

**Step 5: Record timestamps and latencies in the ImpVM reconciler**

In `internal/controller/impvm_controller.go`, when transitioning to Scheduled:
```go
if vm.Status.ScheduledAt == nil {
    now := metav1.Now()
    vm.Status.ScheduledAt = &now
    if !vm.CreationTimestamp.IsZero() {
        ObserveSchedulingLatency(now.Sub(vm.CreationTimestamp.Time))
    }
}
```

When transitioning to Running:
```go
if vm.Status.RunningAt == nil {
    now := metav1.Now()
    vm.Status.RunningAt = &now
    if vm.Status.ScheduledAt != nil {
        ObserveBootLatency(now.Sub(vm.Status.ScheduledAt.Time))
    }
}
```

**Step 6: Run all tests**

```bash
KUBEBUILDER_ASSETS="..." go test ./...
```

**Step 7: Commit**

```bash
git add api/v1alpha1/impvm_types.go internal/controller/metrics.go internal/controller/metrics_test.go internal/controller/impvm_controller.go
git commit -m "feat(metrics): operator scheduling + boot latency histograms, timestamps in ImpVM status"
```

---

### Task 9: Node agent /metrics endpoint

**Files:**
- Modify: `internal/agent/reconciler.go` (add metrics collection goroutine)
- Create: `internal/agent/metrics.go`
- Create: `internal/agent/metrics_test.go`

**Step 1: Write the failing test** (`internal/agent/metrics_test.go`)

```go
//go:build linux

package agent_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syscode-labs/imp/internal/agent"
)

func TestMetricsHandler_serves(t *testing.T) {
	h := agent.NewMetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "go_") {
		t.Error("expected Prometheus default Go metrics in response")
	}
}

func TestVMMetrics_registered(t *testing.T) {
	collector := agent.NewVMMetricsCollector()
	// Register a fake VM
	collector.SetVMState("default/test-vm", "Running", "test-node", "small")
	h := agent.NewMetricsHandlerWithCollector(collector)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "imp_vm_state") {
		t.Errorf("expected imp_vm_state in metrics output, got:\n%s", w.Body.String())
	}
}
```

**Step 2: Create `internal/agent/metrics.go`**

```go
//go:build linux

package agent

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const metricsPort = ":9090"

// VMMetricsCollector holds per-VM metric state for the node agent.
type VMMetricsCollector struct {
	vmState *prometheus.GaugeVec
	guestCPU *prometheus.GaugeVec
	guestMem *prometheus.GaugeVec
	guestDisk *prometheus.GaugeVec
	reg      *prometheus.Registry
}

// NewVMMetricsCollector creates a new collector with its own registry.
func NewVMMetricsCollector() *VMMetricsCollector {
	reg := prometheus.NewRegistry()
	c := &VMMetricsCollector{
		vmState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imp_vm_state",
			Help: "Current VM state (1 = active state).",
		}, []string{"impvm", "namespace", "node", "state"}),
		guestCPU: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imp_vm_guest_cpu_usage_ratio",
			Help: "Guest VM CPU usage ratio (0.0–1.0).",
		}, []string{"impvm", "namespace", "node", "impvmclass"}),
		guestMem: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imp_vm_guest_memory_used_bytes",
			Help: "Guest VM memory used bytes.",
		}, []string{"impvm", "namespace", "node", "impvmclass"}),
		guestDisk: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imp_vm_guest_disk_used_bytes",
			Help: "Guest VM root disk used bytes.",
		}, []string{"impvm", "namespace", "node", "impvmclass"}),
		reg: reg,
	}
	reg.MustRegister(c.vmState, c.guestCPU, c.guestMem, c.guestDisk)
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return c
}

// SetVMState sets the imp_vm_state gauge for a VM. key = "namespace/name".
func (c *VMMetricsCollector) SetVMState(key, state, node, impvmclass string) {
	ns, name := splitKey(key)
	c.vmState.WithLabelValues(name, ns, node, state).Set(1)
}

// SetGuestMetrics updates guest agent metrics for a VM.
func (c *VMMetricsCollector) SetGuestMetrics(key, node, impvmclass string, cpu float64, mem, disk int64) {
	ns, name := splitKey(key)
	c.guestCPU.WithLabelValues(name, ns, node, impvmclass).Set(cpu)
	c.guestMem.WithLabelValues(name, ns, node, impvmclass).Set(float64(mem))
	c.guestDisk.WithLabelValues(name, ns, node, impvmclass).Set(float64(disk))
}

// ClearVM removes all metric series for a VM when it's deleted.
func (c *VMMetricsCollector) ClearVM(key string) {
	ns, name := splitKey(key)
	c.vmState.DeletePartialMatch(prometheus.Labels{"impvm": name, "namespace": ns})
	c.guestCPU.DeletePartialMatch(prometheus.Labels{"impvm": name, "namespace": ns})
	c.guestMem.DeletePartialMatch(prometheus.Labels{"impvm": name, "namespace": ns})
	c.guestDisk.DeletePartialMatch(prometheus.Labels{"impvm": name, "namespace": ns})
}

// NewMetricsHandler returns an HTTP handler for the default Prometheus registry.
func NewMetricsHandler() http.Handler {
	return promhttp.Handler()
}

// NewMetricsHandlerWithCollector returns an HTTP handler for the given collector's registry.
func NewMetricsHandlerWithCollector(c *VMMetricsCollector) http.Handler {
	return promhttp.HandlerFor(c.reg, promhttp.HandlerOpts{})
}

func splitKey(key string) (ns, name string) {
	for i, ch := range key {
		if ch == '/' {
			return key[:i], key[i+1:]
		}
	}
	return "", key
}
```

**Step 3: Start the metrics server in the agent reconciler**

In `internal/agent/reconciler.go`, in the reconciler's `Start()` or setup function, add:

```go
go func() {
    mux := http.NewServeMux()
    mux.Handle("/metrics", agent.NewMetricsHandlerWithCollector(r.metricsCollector))
    if err := http.ListenAndServe(metricsPort, mux); err != nil {
        log.Error(err, "metrics server failed")
    }
}()
```

Add `metricsCollector *VMMetricsCollector` to the reconciler struct. Update the metrics collector from the VM reconcile loop when VM phase changes and when VSOCK metrics are available.

**Step 4: Run tests**

```bash
go test ./internal/agent/... -v -run TestMetrics
```

Expected: both metrics tests PASS.

**Step 5: Run all tests**

```bash
KUBEBUILDER_ASSETS="..." go test ./...
```

**Step 6: Commit**

```bash
git add internal/agent/metrics.go internal/agent/metrics_test.go internal/agent/reconciler.go
git commit -m "feat(metrics): node agent /metrics on :9090 — imp_vm_state, guest CPU/mem/disk"
```

---

### Task 10: Helm chart — ServiceMonitor, PodMonitor, metrics port

**Files:**
- Modify: `charts/imp/values.yaml`
- Create: `charts/imp/templates/operator/servicemonitor.yaml`
- Create: `charts/imp/templates/agent/podmonitor.yaml`
- Modify: `charts/imp/templates/agent/daemonset.yaml` (add metrics port)
- Create: `charts/imp/tests/metrics_test.yaml`

**Step 1: Write the failing helm test** (`charts/imp/tests/metrics_test.yaml`)

```yaml
suite: metrics resources
templates:
  - templates/operator/servicemonitor.yaml
  - templates/agent/podmonitor.yaml
tests:
  - it: creates ServiceMonitor by default
    template: templates/operator/servicemonitor.yaml
    asserts:
      - isKind:
          of: ServiceMonitor
      - equal:
          path: spec.endpoints[0].port
          value: metrics

  - it: skips ServiceMonitor when disabled
    template: templates/operator/servicemonitor.yaml
    set:
      metrics.serviceMonitor.enabled: false
    asserts:
      - hasDocuments:
          count: 0

  - it: creates PodMonitor by default
    template: templates/agent/podmonitor.yaml
    asserts:
      - isKind:
          of: PodMonitor
      - equal:
          path: spec.podMetricsEndpoints[0].port
          value: metrics

  - it: skips PodMonitor when disabled
    template: templates/agent/podmonitor.yaml
    set:
      metrics.serviceMonitor.enabled: false
    asserts:
      - hasDocuments:
          count: 0
```

**Step 2: Run test, verify it fails**

```bash
helm unittest charts/imp 2>&1 | tail -5
```

**Step 3: Add metrics values to `charts/imp/values.yaml`**

```yaml
metrics:
  serviceMonitor:
    enabled: true
    interval: 30s
```

**Step 4: Create `charts/imp/templates/operator/servicemonitor.yaml`**

```yaml
{{- if .Values.metrics.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "imp.fullname" . }}-operator
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
    app.kubernetes.io/component: operator
spec:
  selector:
    matchLabels:
      {{- include "imp.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: operator
  endpoints:
    - port: metrics
      interval: {{ .Values.metrics.serviceMonitor.interval }}
  namespaceSelector:
    matchNames:
      - {{ .Release.Namespace }}
{{- end }}
```

**Step 5: Create `charts/imp/templates/agent/podmonitor.yaml`**

```yaml
{{- if .Values.metrics.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: {{ include "imp.fullname" . }}-agent
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
    app.kubernetes.io/component: agent
spec:
  selector:
    matchLabels:
      {{- include "imp.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: agent
  podMetricsEndpoints:
    - port: metrics
      interval: {{ .Values.metrics.serviceMonitor.interval }}
  namespaceSelector:
    matchNames:
      - {{ .Release.Namespace }}
{{- end }}
```

**Step 6: Add metrics port to agent DaemonSet**

In `charts/imp/templates/agent/daemonset.yaml`, add to the container ports:
```yaml
        - name: metrics
          containerPort: 9090
          protocol: TCP
```

**Step 7: Run helm tests**

```bash
helm unittest charts/imp
```

Expected: all tests pass (now 34 total: 30 existing + 4 new).

Also lint:
```bash
helm lint charts/imp-crds charts/imp
```

**Step 8: Commit**

```bash
git add charts/imp/values.yaml charts/imp/templates/operator/servicemonitor.yaml charts/imp/templates/agent/podmonitor.yaml charts/imp/templates/agent/daemonset.yaml charts/imp/tests/metrics_test.yaml
git commit -m "feat(helm): ServiceMonitor + PodMonitor for Prometheus Operator (default enabled)"
```

---

### Task 11: Layer 1 E2E tests + CI job

**Files:**
- Modify: `test/e2e/e2e_suite_test.go`
- Modify: `test/e2e/e2e_test.go` (replace kubebuilder scaffold with real tests)
- Modify: `.github/workflows/ci.yml` (add Kind E2E job)

**Step 1: Update `test/e2e/e2e_suite_test.go`**

Replace the existing BeforeSuite/AfterSuite to use Helm + Kind instead of `make deploy`:

```go
//go:build e2e

package e2e

import (
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/syscode-labs/imp/test/utils"
)

const (
	namespace     = "imp-system"
	helmRelease   = "imp"
	helmCRDRelease = "imp-crds"
)

var _ = BeforeSuite(func() {
	By("creating kind cluster")
	cmd := exec.Command("kind", "create", "cluster", "--name", "imp-e2e")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	By("creating namespace")
	cmd = exec.Command("kubectl", "create", "ns", namespace)
	_, _ = utils.Run(cmd)

	By("installing imp-crds chart")
	cmd = exec.Command("helm", "install", helmCRDRelease, "charts/imp-crds",
		"--namespace", namespace, "--wait")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "helm install imp-crds failed")

	By("installing imp chart")
	cmd = exec.Command("helm", "install", helmRelease, "charts/imp",
		"--namespace", namespace,
		"--set", "agent.env.kernelPath=/usr/local/bin/firecracker",
		"--set", "metrics.serviceMonitor.enabled=false", // no Prometheus Operator in kind
		"--wait", "--timeout", "2m")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "helm install imp failed")
})

var _ = AfterSuite(func() {
	By("uninstalling imp chart")
	cmd := exec.Command("helm", "uninstall", helmRelease, "--namespace", namespace)
	_, _ = utils.Run(cmd)

	By("uninstalling imp-crds chart")
	cmd = exec.Command("helm", "uninstall", helmCRDRelease, "--namespace", namespace)
	_, _ = utils.Run(cmd)

	By("deleting kind cluster")
	cmd = exec.Command("kind", "delete", "cluster", "--name", "imp-e2e")
	_, _ = utils.Run(cmd)
})

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Imp E2E Suite")
}
```

**Step 2: Rewrite `test/e2e/e2e_test.go`**

Replace the entire file with:

```go
//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/syscode-labs/imp/test/utils"
)

var _ = Describe("Imp operator", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("CRDs", func() {
		It("installs all six CRDs", func() {
			crds := []string{
				"impvms.imp.dev",
				"impvmclasses.imp.dev",
				"impvmtemplates.imp.dev",
				"impnetworks.imp.dev",
				"clusterimpconfigs.imp.dev",
				"clusterimpnodeprofiles.imp.dev",
			}
			for _, crd := range crds {
				cmd := exec.Command("kubectl", "get", "crd", crd)
				_, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CRD %s not found", crd))
			}
		})
	})

	Context("Operator", func() {
		It("starts and passes health checks", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/name=imp,app.kubernetes.io/instance=%s,app.kubernetes.io/component=operator", helmRelease),
					"-n", namespace,
					"-o", "jsonpath={.items[0].status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Running"))
			}).Should(Succeed())
		})
	})

	Context("Webhooks", func() {
		It("rejects an ImpVM with missing classRef and missing image", func() {
			manifest := `
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: invalid-vm
  namespace: default
spec: {}
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "expected webhook to reject invalid ImpVM")
		})
	})

	Context("ImpVM CRUD", func() {
		const vmName = "e2e-smoke"
		AfterEach(func() {
			exec.Command("kubectl", "delete", "impvm", vmName, "-n", "default", "--ignore-not-found").Run()
		})

		It("creates and lists an ImpVM", func() {
			manifest := fmt.Sprintf(`
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: %s
  namespace: default
spec:
  classRef:
    name: small
  image: ghcr.io/syscode-labs/test:latest
  lifecycle: ephemeral
`, vmName)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "impvm", vmName, "-n", "default",
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(vmName))
			}).Should(Succeed())
		})

		It("operator remains running after ImpVM creation", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/instance=%s,app.kubernetes.io/component=operator", helmRelease),
					"-n", namespace, "-o", "jsonpath={.items[0].status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Running"))
			}).Should(Succeed())
		})
	})

	Context("Metrics", func() {
		It("operator /metrics endpoint responds 200", func() {
			// Port-forward the operator service
			pf := exec.Command("kubectl", "port-forward",
				fmt.Sprintf("svc/%s-imp-operator", helmRelease),
				"18080:8080", "-n", namespace)
			pf.Start()
			defer pf.Process.Kill()
			time.Sleep(2 * time.Second) // wait for port-forward to be ready

			Eventually(func(g Gomega) {
				resp, err := http.Get("http://localhost:18080/metrics")
				g.Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				g.Expect(resp.StatusCode).To(Equal(200))
			}).Should(Succeed())
		})
	})
})
```

**Step 3: Add Kind E2E CI job to `.github/workflows/ci.yml`**

Add a new job after the existing `test` job:

```yaml
  e2e-kind:
    name: E2E (Kind)
    runs-on: ubuntu-latest
    needs: [lint, build]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: helm/kind-action@v1
        with:
          cluster_name: imp-e2e
      - name: Install Helm
        uses: azure/setup-helm@v4
      - name: Run E2E tests
        run: go test -tags e2e ./test/e2e/... -v -timeout 15m
```

**Step 4: Verify E2E test compiles**

```bash
go build -tags e2e ./test/e2e/...
```

Expected: compiles without errors (cannot run without Kind).

**Step 5: Run all unit tests to verify no regressions**

```bash
KUBEBUILDER_ASSETS="..." go test ./...
```

**Step 6: Commit**

```bash
git add test/e2e/ .github/workflows/ci.yml
git commit -m "feat(e2e): Layer 1 Kind-based E2E — CRDs, operator health, webhooks, CRUD, metrics"
```
