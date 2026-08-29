# Sandbox Gateway API

Status: internal preview. Not a supported external API yet.

The Sandbox Gateway is the data-plane boundary for executing commands and
managing files in an `ImpSandbox` guest. It is implemented as a node-local
gRPC service that proxies requests to the guest agent over the host's VSOCK
socket.

This page records the current wire contract and its safety requirements. It is
not a TypeScript SDK guide: `sdk/typescript/` does not exist yet. A supported
SDK quickstart will be published only after the production-readiness work in
OpenSpec task B.1.1 is complete.

## Release Status

The RPC implementation and its unit/integration coverage exist, but the chart
does not yet provision the guest control credential required by the gateway.
The gateway also exposes plaintext gRPC through a node `hostPort`; it has no
public endpoint discovery, TLS, or mTLS configuration.

Do not expose port `9600` outside a trusted cluster network. Do not build a
production integration on this API. The release gate is:

1. Per-guest control-token delivery to the gateway and guest.
2. Protected endpoint discovery plus TLS or mTLS transport.
3. A chart-installed end-to-end gateway test.
4. A published, versioned TypeScript SDK and executable quickstart.

## Contract Source

The authoritative API definition is
[`internal/proto/sandboxgateway/gateway.proto`](../../internal/proto/sandboxgateway/gateway.proto).
Generated names, field numbers, and service paths come from that file. The
current service name is `sandboxgateway.SandboxGateway`.

The gateway implements native gRPC with server reflection and the standard
gRPC health service. It does **not** implement Connect RPC or a JSON/REST
surface today. Those are design proposals, not current behavior.

## Data Path

```
client -> node-local gRPC gateway -> /run/imp/<namespace>-<vm>.vsock -> guest agent
```

The gateway runs only on nodes selected with `imp/enabled=true`. A request can
reach only a guest on the same node as that gateway instance. Future SDKs will
resolve the backing `ImpVM` node and select the corresponding gateway; callers
must not guess or scan node endpoints.

## Authentication And Authorization

Each sandbox controller mints a session token into the Secret named by
`ImpSandbox.status.sessionSecretRef`, in the sandbox's namespace. Kubernetes
RBAC controls who may read that Secret.

Every gateway request requires exactly one value for each gRPC metadata key:

| Metadata key | Value |
| --- | --- |
| `authorization` | `Bearer <session-token>` |
| `imp-sandbox-uid` | `ImpSandbox.metadata.uid` |
| `imp-sandbox-namespace` | sandbox namespace |
| `imp-sandbox-vm-name` | backing VM name |

The token is an HMAC bound to all three identity fields: sandbox UID,
namespace, and VM name. The gateway compares the authenticated scope with the
request's `VMRef` before opening a VSOCK connection. A valid token for one
sandbox cannot access a different sandbox, including one scheduled on the same
node.

Never log session tokens or attach them to URLs. Treat them as short-scope
bearer credentials and grant read access to the session Secret only to the
workload or principal operating that sandbox.

## RPC Reference

Every request contains `VMRef`:

```proto
message VMRef {
  string namespace = 1;
  string vm_name = 2;
}
```

| RPC | Type | Purpose |
| --- | --- | --- |
| `Exec` | Server streaming | Runs `command`; returns stdout/stderr frames and one terminal frame with `final=true` and `exit_code`. |
| `ReadFile` | Unary | Reads a guest file, returning content, total size, and truncation state. |
| `WriteFile` | Unary | Writes or appends guest file content; returns bytes written. |
| `ListDir` | Unary | Lists one directory level. |
| `Stat` | Unary | Returns size, mode, type, and modification time. |
| `Remove` | Unary | Deletes a file or empty directory; `recursive=true` permits recursive removal. |

`Exec` has a server-streaming signature for forward compatibility. In the
current implementation guest execution is buffered, then the gateway emits at
most one stdout frame, one stderr frame, and one final frame. Do not assume
incremental output latency until the streaming implementation is released.

## Limits And Timeouts

| Concern | Current behavior |
| --- | --- |
| File read/write payload | 1 MiB maximum in the guest agent. Oversize operations return `ResourceExhausted`. |
| Exec timeout | `timeout_seconds` is capped at 60 seconds; zero or out-of-range values use the 60-second cap. |
| File transfer | No chunked transfer or upload/download resumability. |
| Paths | The guest agent enforces file-operation behavior. Callers must not rely on host-path access; the gateway mounts the host socket directory read-only only. |

## Errors And Retries

The gateway returns standard gRPC status codes. Follow the gRPC status-code
guidance: retry only transient failures and never automatically retry a
non-idempotent write or command without an application-level idempotency plan.

| Code | Meaning | Caller action |
| --- | --- | --- |
| `Unauthenticated` | Missing or invalid scoped bearer metadata. | Refresh credentials and verify all metadata values. |
| `PermissionDenied` | `VMRef` does not match the authenticated sandbox scope. | Do not retry; correct the target sandbox. |
| `InvalidArgument` | Missing VM identity, command, or other malformed request data. | Correct the request. |
| `Unavailable` | The guest socket is absent from the selected node. | Re-resolve placement, then retry with bounded backoff. |
| `FailedPrecondition` | The guest control session cannot be opened or gateway guest credential is absent. | Fix deployment/session state before retrying. |
| `ResourceExhausted` | File payload exceeds the 1 MiB limit. | Split data outside the current API, or use a supported transfer mechanism when released. |
| `DeadlineExceeded` | The caller or execution deadline expired. | Determine whether the operation may have completed before retrying. |

Reference: [gRPC status codes](https://grpc.io/docs/guides/status-codes/).

## Kubernetes Prerequisites

Before a future SDK can operate a sandbox, it will need read access to:

1. The target `ImpSandbox`, to read its UID and `status.sessionSecretRef`.
2. The referenced Secret's `data.token` in the same namespace.
3. The backing `ImpVM`, to resolve its node placement.

Grant these permissions narrowly by namespace and resource name where your RBAC
model permits it. A client must never receive cluster-wide Secret list access
solely to operate a sandbox.

## Support And Compatibility

The proto package is internal and may change without compatibility guarantees
until an SDK release declares a versioned public surface. The following are
not available in this preview:

- TypeScript or Go SDK packages.
- Public ingress, browser access, REST, JSON, or Connect RPC.
- Background process handles or reattachment.
- Chunked files larger than 1 MiB.
- Production endpoint discovery, TLS, or mTLS.

When the SDK is released, this page will define its supported version range,
deprecation policy, migration notes, and runnable examples.
