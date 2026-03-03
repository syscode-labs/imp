# Phase 1 Completion Design

**Goal:** Implement the three remaining Phase 1 features: VSOCK guest agent + probes, Prometheus metrics, and E2E tests.

---

## 1. VSOCK Guest Agent + Probes

### Architecture

The guest agent is a binary that runs inside each VM. The node agent injects it transparently at boot — no image modification required. When `guestAgent.enabled: false`, the VM boots bare with no injection and no probe support.

```
Node Agent                          VM (ext4 rootfs)
┌─────────────────────┐             ┌──────────────────────────┐
│ rootfs.Inject()     │─── ext4 ───▶│ /.imp/guest-agent        │
│                     │             │ /.imp/init               │
│ VSOCKClient         │◀── gRPC ───▶│ gRPC server (port 10000) │
│   Exec()            │             │   Exec handler           │
│   HTTPCheck()       │             │   HTTPCheck handler      │
│   Metrics()         │             │   Metrics handler        │
└─────────────────────┘             └──────────────────────────┘
         ▲
         │ Firecracker Unix socket
         │ /run/imp/sockets/{vmid}.vsock
```

### New packages and files

| Path | Purpose |
|------|---------|
| `cmd/guest-agent/main.go` | Binary that runs inside the VM |
| `internal/proto/guest/guest.proto` | gRPC service definition |
| `internal/proto/guest/` | Generated protobuf + gRPC Go code |
| `internal/guest/` | Server handlers (Exec, HTTPCheck, Metrics) |
| `internal/agent/vsock/client.go` | Host-side gRPC client over VSOCK |
| `internal/agent/probe/runner.go` | Probe polling goroutine per VM |

The `imp-guest-agent` binary is embedded in the `imp-agent` container image using `//go:embed`. The node agent extracts it on startup to a temp path for injection.

### OCI injection

`internal/agent/rootfs/` gains an `Inject(guestAgentBytes []byte, ext4Path string) error` function that appends two files to the ext4 image after OCI→ext4 conversion:

- `/.imp/guest-agent` — the agent binary (mode 0755)
- `/.imp/init` — init wrapper (mode 0755):

```sh
#!/bin/sh
/.imp/guest-agent &
exec /sbin/init "$@"
```

When `guestAgent.enabled` (default), the node agent:
1. Calls `rootfs.Inject()` after image conversion
2. Appends `init=/.imp/init` to `kernel_args`

### gRPC service definition

```protobuf
syntax = "proto3";
package guest;
option go_package = "github.com/syscode-labs/imp/internal/proto/guest";

service GuestAgent {
  rpc Exec(ExecRequest) returns (ExecResponse);
  rpc HTTPCheck(HTTPCheckRequest) returns (HTTPCheckResponse);
  rpc Metrics(MetricsRequest) returns (MetricsResponse);
}

message ExecRequest  { repeated string command = 1; int32 timeout_seconds = 2; }
message ExecResponse { int32 exit_code = 1; string stdout = 2; string stderr = 3; }

message HTTPCheckRequest  { int32 port = 1; string path = 2; map<string,string> headers = 3; int32 timeout_seconds = 4; }
message HTTPCheckResponse { int32 status_code = 1; }

message MetricsRequest  {}
message MetricsResponse {
  double cpu_usage_ratio    = 1;  // 0.0–1.0
  int64  memory_used_bytes  = 2;
  int64  disk_used_bytes    = 3;
}
```

### VSOCK connection

Firecracker exposes VSOCK via a Unix domain socket at `/run/imp/sockets/{vmid}.vsock`. To dial guest port 10000, the client connects to the Unix socket and sends `CONNECT 10000\n` before the gRPC handshake. `internal/agent/vsock/client.go` wraps this as a `net.Conn` and wires it into the gRPC `WithContextDialer`.

### Probe runner

`internal/agent/probe/runner.go` starts one goroutine per VM after it reaches `Running`. On each probe's period it calls the appropriate RPC, evaluates success/failure, and patches `ImpVM.status.conditions`:

- `StartupProbe` — blocks readiness until passing; failures don't count against liveness
- `ReadinessProbe` — sets `Ready` condition
- `LivenessProbe` — sets `Healthy` condition; repeated failures trigger VM restart

### Opt-out API field

Added to `ImpVMClassSpec`, `ImpVMTemplateSpec`, and `ImpVMSpec` (same inheritance as probes):

```go
type GuestAgentConfig struct {
    // Enabled controls guest agent injection. Defaults to true.
    // Set to false for bare VMs that do not need probes or VM-side metrics.
    // +optional
    Enabled *bool `json:"enabled,omitempty"`
}
```

Resolved order: ImpVM → ImpVMTemplate → ImpVMClass → default (true).

---

## 2. Prometheus Metrics

### Node agent: `/metrics` on port 9090

One endpoint per node (DaemonSet pod), aggregating all VMs on that node. Scraped by a `PodMonitor`.

**From Firecracker API** (polled every 15s per running VM):

| Metric | Type | Description |
|--------|------|-------------|
| `imp_vm_vcpu_time_seconds_total` | Counter | vCPU execution time |
| `imp_vm_memory_balloon_bytes` | Gauge | Memory balloon size |

**From guest agent VSOCK** (polled every 15s; skipped when `guestAgent.enabled: false`):

| Metric | Type | Description |
|--------|------|-------------|
| `imp_vm_guest_cpu_usage_ratio` | Gauge | CPU usage 0.0–1.0 |
| `imp_vm_guest_memory_used_bytes` | Gauge | RSS memory used |
| `imp_vm_guest_disk_used_bytes` | Gauge | Root disk used |

All node-agent metrics carry labels: `impvm`, `namespace`, `node`, `impvmclass`.

**VM lifecycle state gauge** (always):

| Metric | Type | Description |
|--------|------|-------------|
| `imp_vm_state` | Gauge | 1 for current state, labels: `impvm`, `namespace`, `node`, `state` |

### Operator: `:8080/metrics`

controller-runtime already exposes reconcile counters and latency. We register two additional histograms at manager startup:

| Metric | Type | Description |
|--------|------|-------------|
| `imp_vm_scheduling_latency_seconds` | Histogram | Pending → Scheduled |
| `imp_vm_boot_latency_seconds` | Histogram | Scheduled → Running |

Timestamps recorded by the operator into `ImpVM.status.timestamps` (new fields); the histogram observations happen when the agent patches status to `Running`.

### Helm chart additions

New `values.yaml` section (default enabled):

```yaml
metrics:
  serviceMonitor:
    enabled: true
    interval: 30s
```

New templates (both gated on `metrics.serviceMonitor.enabled`):
- `charts/imp/templates/operator/servicemonitor.yaml` — `ServiceMonitor` targeting operator port `8080`
- `charts/imp/templates/agent/podmonitor.yaml` — `PodMonitor` targeting agent port `9090`

Agent DaemonSet and Service get a new `metrics` port (`9090`).

---

## 3. E2E Tests

### Two-layer approach

**Layer 1 — Operator E2E** (`//go:build e2e`, runs in Kind):

Deploys the Helm chart into a Kind cluster. No Firecracker, no KVM needed. Tests:
- CRD installation (all 6 CRDs present, schema validates)
- Operator pod starts and passes `/healthz` + `/readyz`
- Webhook accepts valid ImpVM, rejects invalid (missing classRef)
- ImpVM CRUD: create → reconciler processes without crash, delete → finalizer runs
- Metrics endpoint responds `200 OK` with `imp_vm_` prefixed metrics

**Layer 2 — Full E2E** (`//go:build e2e_full`, self-hosted KVM runner):

Requires a node with `/dev/kvm`. Tests:
- Full VM boot: ImpVM reaches `Running` state within 60s
- Guest agent connects: `Exec(["echo", "hello"])` returns exit 0
- Probes pass: startup probe succeeds, `Ready` condition set
- Metrics contain real values: `imp_vm_guest_cpu_usage_ratio > 0`
- Teardown: VM deleted → TAP interface removed, bridge cleaned up

### File layout

```
test/
├── e2e/
│   ├── e2e_suite_test.go   # BeforeSuite: kind create + helm install
│   ├── e2e_test.go         # Layer 1 tests (replaces kubebuilder stub)
│   └── utils/              # shared helpers (already exists)
└── e2e_full/
    ├── suite_test.go        # BeforeSuite: verify /dev/kvm, deploy agent
    └── vm_lifecycle_test.go # Layer 2 tests
```

### CI integration

`ci.yml` already has an E2E job gated on `vars.E2E_RUNNER_LABEL`. Layer 1 runs on `ubuntu-latest` (no gate needed — add a new job). Layer 2 runs on the self-hosted runner under the existing gate.

```yaml
# New job in ci.yml
e2e-kind:
  runs-on: ubuntu-latest
  steps:
    - uses: helm/kind-action@v1
    - run: go test -tags e2e ./test/e2e/ -v -timeout 10m
```
