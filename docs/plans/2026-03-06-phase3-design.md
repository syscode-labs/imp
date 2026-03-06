# Imp — Phase 3 Design

> *CI runner pools, cross-node VM networking, and Cilium mesh integration*

**Goal:** Make Imp useful for CI workloads (ephemeral runner VMs that pick up jobs and
disappear), give VMs first-class network connectivity across nodes, and enroll VMs into
Cilium so they behave like pods from a security and observability standpoint.

**Scope:** Three independent areas. Each can ship separately. Cross-node networking and
Cilium enrollment are tightly related and share infrastructure; CI runners are fully
independent.

---

## 1. CI Runner Pools (`ImpVMRunnerPool`)

### What it does

A runner pool provisions ephemeral VMs that register with a CI platform, pick up exactly
one job, then terminate. The pool keeps a configurable number of idle VMs pre-registered
and waiting, scaling up to a maximum when demand grows.

Think of it like a vending machine for CI runners: you set how many to keep stocked
(`minIdle`) and the maximum that can run at once (`maxConcurrent`). Jobs come in, a VM
gets assigned, the job runs, the VM is gone.

### Runner software

Runner software (the GitHub Actions runner binary, `gitlab-runner`, etc.) is delivered
as an OCI image layer maintained by the Imp project:

```
ghcr.io/syscode-labs/imp-runners/github-actions:<version>
ghcr.io/syscode-labs/imp-runners/gitlab:<version>
ghcr.io/syscode-labs/imp-runners/forgejo:<version>
```

At VM creation time the rootfs builder composites the user's base image with the runner
layer — the result is cached on the node. No downloads happen inside the running VM.

Users who prefer to bake the runner binary into their own image can omit `runnerLayer`
entirely; the operator will use the base image as-is.

### ImpVMRunnerPool CRD

```yaml
apiVersion: imp.dev/v1alpha1
kind: ImpVMRunnerPool
metadata:
  name: ci-linux-small
  namespace: ci
spec:
  # VM shape — reuses an existing ImpVMTemplate.
  templateName: ubuntu-runner

  platform:
    # Supported: github-actions | gitlab | forgejo
    type: github-actions
    # Required for GitLab and Forgejo; leave empty for github.com.
    serverURL: ""
    scope:
      # Register as an org-level runner.
      org: my-org
      # Or as a repo-level runner:
      # repo: owner/repo
    # Secret must contain the registration token or a PAT with runner:write scope.
    credentialsSecret: gh-runner-creds

  # OCI layer with the runner binary. Omit if already in the base image.
  runnerLayer: ghcr.io/syscode-labs/imp-runners/github-actions:v2.317

  # Labels shown to the CI platform.
  labels: [self-hosted, linux, firecracker]

  scaling:
    # Pre-registered idle VMs. 0 = pure on-demand (no idle cost). Default: 0.
    minIdle: 0
    # Hard cap on simultaneous runner VMs.
    maxConcurrent: 10

  jobDetection:
    webhook:
      # Platform sends job events here. Requires an ingress/LoadBalancer.
      enabled: true
      # HMAC secret for payload verification.
      secretRef: gh-webhook-secret
    polling:
      # Fallback: operator polls the platform API for queued jobs.
      enabled: true
      intervalSeconds: 30
```

### Status

```go
type ImpVMRunnerPoolStatus struct {
    IdleCount    int32            `json:"idleCount,omitempty"`
    ActiveCount  int32            `json:"activeCount,omitempty"`
    Conditions   []metav1.Condition `json:"conditions,omitempty"`
}
```

### Lifecycle

1. Operator creates `minIdle` VMs. Each boots, the runner init wrapper reads config
   injected via VSOCK (registration URL, JIT token), registers with the platform, and
   enters a waiting state.
2. A job arrives (webhook or poll). The operator claims an idle VM by patching its
   config with a JIT token scoped to that job. If no idle VM is available and
   `activeCount < maxConcurrent`, a new VM is created.
3. The runner picks up the job. When it finishes (success or failure) the runner
   process exits, the agent sets `phase=Succeeded`, and the VM is deleted.
4. The operator reconciles back to `minIdle`.

### Platform drivers

Each platform has a thin driver that knows how to:
- Exchange the credential for a JIT/one-time registration token
- Parse the platform's webhook payload to detect queued jobs
- Poll the platform API for queue depth

GitHub Actions and Forgejo share a driver (Forgejo implements the GitHub Actions API).
GitLab has its own driver.

### Job detection

Webhook is the primary path (low latency, ~instant). Polling is the fallback (works
without ingress, ~30 s latency). Both can be enabled simultaneously; whichever fires
first wins.

---

## 2. Cross-node VM Networking

### What it does

Today, VMs on different nodes cannot reach each other. This area makes VMs that share an
`ImpNetwork` reachable from anywhere in the cluster — other VMs, pods, and Kubernetes
services — regardless of which node they land on. No configuration is needed beyond
putting VMs in the same `ImpNetwork`.

### Two paths: Cilium and non-Cilium

The existing CNI detection (`cniDetectRunnable`) already identifies whether Cilium is
present. Cross-node networking uses this to choose a path automatically.

#### Path A — Cilium present

VMs are enrolled as `CiliumExternalWorkload` objects. Cilium handles all overlay
tunnelling (Geneve or WireGuard depending on cluster config). The VM gets a stable
Cilium identity and becomes a full mesh participant:

- Reachable from any pod or VM on the cluster
- Subject to `NetworkPolicy` (same YAML as pods, no changes needed)
- Visible in Hubble (`hubble observe` shows VM flows)
- Can reach `ClusterIP` services via kube-dns

The `ImpNetworkController` creates one `CiliumExternalWorkload` per VM. The node agent
starts the bundled `cilium-agent` inside the VM (from an OCI layer — see Section 3)
with config injected via VSOCK.

#### Path B — No Cilium

Imp manages a lightweight VXLAN overlay:

- Each node runs a `impvxlan0` interface (VNI derived from `ImpNetwork` UID)
- On VM start, the agent registers the VM's MAC and IP in `ImpNetwork.status.vtepTable`
- The operator propagates VTEP entries to all nodes holding members of that network
- Each agent reconciles its local FDB on change

This gives VM-to-VM connectivity across nodes without NetworkPolicy or Hubble.

### ImpNetwork status additions

```go
type VTEPEntry struct {
    NodeIP string `json:"nodeIP"`
    VMIP   string `json:"vmIP"`
    VMMAC  string `json:"vmMAC"`
}

// Added to ImpNetworkStatus:
VTEPTable []VTEPEntry `json:"vtepTable,omitempty"`
```

### Testing

Two separate multi-node Kind clusters in CI:

- `kind-cilium`: Kind + Cilium CNI (installed via `cilium install`). Validates
  `CiliumExternalWorkload` enrollment, cross-node VM-to-VM ping, VM-to-pod ping,
  and Hubble flow visibility.
- `kind-flannel`: Kind + default flannel. Validates the VXLAN fallback path:
  cross-node VM-to-VM ping, VM-to-pod ping via kube-proxy.

---

## 3. Cilium Mesh Integration

### What it does

When Cilium is present, VMs become full Cilium citizens: they have a network identity,
firewall rules (NetworkPolicy) apply to them, and their traffic appears in Hubble — the
same as any pod, with no changes to existing workloads.

In plain terms: *add one line to your VM template and your VM joins the cluster's network
mesh automatically — firewall rules, monitoring, and service discovery all work without
touching the VM image.*

### cilium-agent OCI layer

Imp publishes a minimal OCI layer containing the `cilium-agent` binary:

```
ghcr.io/syscode-labs/imp-cilium-agent:<cilium-version>
```

Built in Imp's CI by extracting the binary from the upstream `cilium/cilium` image.
Versioned to match Cilium releases. Updated via Dependabot or a manual trigger.

A build script is provided at `hack/build-cilium-layer.sh` for users who want to build
and host their own layer.

Users add the layer to their VM template:

```yaml
spec:
  ciliumLayer: ghcr.io/syscode-labs/imp-cilium-agent:v1.16
```

At boot the rootfs builder composites the base image with this layer. The node agent
injects Cilium config (cluster endpoint, node IP, identity labels) via VSOCK.
`cilium-agent` starts from what is already in the rootfs — no downloads, no installs.

### Enrollment flow

1. VM is scheduled. `ImpNetworkController` creates a `CiliumExternalWorkload` with the
   VM's allocated IP range.
2. VM boots. Agent injects Cilium config via VSOCK. `cilium-agent` starts and dials
   the node's `cilium-agent`.
3. Cilium assigns the VM an identity. NetworkPolicy rules take effect. Hubble starts
   recording flows.
4. On VM deletion, the `CiliumExternalWorkload` is garbage-collected via ownerRef.

### ImpVM status addition

```go
// CiliumEndpointID is set once the VM is enrolled in Cilium. Empty if Cilium is absent.
CiliumEndpointID int64 `json:"ciliumEndpointID,omitempty"`
```

### CNI support policy

Imp provides first-class integration with Cilium. Other CNIs (Flannel, Calico, etc.)
work for basic node-local networking but are not officially supported for cross-node
connectivity or NetworkPolicy. External contributions adding CNI drivers are welcome and
will be reviewed.

This policy is stated clearly in the project README.

---

## Phased delivery

| Area | Dependencies | Notes |
|---|---|---|
| ImpVMRunnerPool | None | Fully independent; can ship first |
| Cross-node networking | None (VXLAN path) / Cilium installed (Cilium path) | Two e2e envs required |
| Cilium mesh | Cross-node networking (Cilium path) | Builds on the enrollment flow |

Each area ships independently. Recommended order: CI runners → cross-node networking →
Cilium mesh.
