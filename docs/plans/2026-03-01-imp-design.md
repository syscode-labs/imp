# Imp — Design Document

> *microVMs that do your bidding*

```
  )\  /(
 (  \/  )    ╦╔╦╗╔═╗
 ( ●  ● )~~✦ ║║║║╠═╝
  \ ‿  /     ╩╩ ╩╚
  (    )~,
  /\  /\     v0.1.0 · imp.dev
```

**Imp** is a Kubernetes operator for Firecracker microVMs. Declare a real isolated
Linux environment the same way you'd declare a pod — it spawns in under a second,
does its job, and disappears. Multi-arch, OCI-native, built for homelabs and cloud alike.

---

## 1. Goals

- Declarative microVM management via Kubernetes CRDs (YAML manifests, `kubectl apply`)
- Firecracker-based: sub-second boot, minimal attack surface, KVM-native
- Multi-arch: `amd64` (Lenovo M720q homelab) and `arm64` (OCI Ampere free tier)
- Kubernetes distribution: Talos Linux
- CNI: Cilium in kube-proxy-free mode (other CNIs supported with degraded integration)
- Ephemeral VMs first (sandboxes, CI runners); persistent VMs in a later phase
- Layered, simple architecture — no unnecessary coupling between layers

### Non-goals (v1)

- VM live migration (snapshot/restore planned for later)
- Persistent VMs (phase 2)
- Cilium external workloads / full IPAM delegation (phase 2)
- Cross-node VM networking / VXLAN overlay (phase 2)

---

## 2. Project Identity

| | |
|---|---|
| **Name** | Imp |
| **API group** | `imp.dev/v1alpha1` |
| **Domain** | `imp.dev` |
| **Repository** | `github.com/syscode-labs/imp` |
| **Language** | Go (kubebuilder / controller-runtime) |
| **Color** | `#e85d04` (Firecracker orange) |

---

## 3. Architecture Layers

```
┌─────────────────────────────────────────────┐
│  Layer 3: API / Config                      │
│  kubectl apply -f myvm.yaml (CRDs)          │
├─────────────────────────────────────────────┤
│  Layer 2: Operator (Deployment, 1 replica)  │
│  Watches CRDs, schedules VMs to nodes,      │
│  detects CNI, manages capacity              │
├─────────────────────────────────────────────┤
│  Layer 1: Node Agent (DaemonSet)            │
│  Owns local Firecracker processes,          │
│  manages TAP devices + nftables NAT,        │
│  updates .status, executes probes via VSOCK │
├─────────────────────────────────────────────┤
│  Layer 0: Talos Node                        │
│  Firecracker binary via system extension    │
│  KVM enabled, /dev/kvm accessible           │
└─────────────────────────────────────────────┘
```

**Key design principle:** the operator never calls Firecracker directly. It sets
`spec.nodeName` on an `ImpVM` object; the node agent on that node reconciles the
actual Firecracker process. This mirrors how the kubelet handles Pods.

---

## 4. CRDs

All resources are under `imp.dev/v1alpha1`.

| Kind | Short | Scope | Purpose |
|------|-------|-------|---------|
| `ImpVM` | `impvm` | Namespace | A microVM instance |
| `ImpVMTemplate` | `impvmt` | Namespace | Reusable VM definition |
| `ImpNetwork` | `impnet` | Namespace | Network topology (NAT, subnet) |
| `ImpVMClass` | `impcls` | Cluster | Compute profiles (vcpu, mem, disk) |
| `ClusterImpConfig` | `impcfg` | Cluster | Operator-wide settings (singleton) |
| `ClusterImpNodeProfile` | `impnp` | Cluster | Per-node capacity overrides |

### 4.1 ImpVMClass

Reusable compute profiles. Follows the `StorageClass` convention — cluster-scoped,
referenced by name.

```yaml
apiVersion: imp.dev/v1alpha1
kind: ImpVMClass
metadata:
  name: small
spec:
  vcpu: 1
  memoryMiB: 512
  diskGiB: 10
  arch: multi          # amd64 | arm64 | multi
  probes:              # default probes, inherited by templates and VMs
    startupProbe:
      exec:
        command: ["systemctl", "is-system-running"]
      initialDelaySeconds: 2
      periodSeconds: 1
      failureThreshold: 30
```

### 4.2 ImpVMTemplate

Reusable VM definition. References an `ImpVMClass` and optionally an `ImpNetwork`.

```yaml
apiVersion: imp.dev/v1alpha1
kind: ImpVMTemplate
metadata:
  name: ubuntu-sandbox
spec:
  classRef:
    name: small
  networkRef:
    name: sandbox-net
  image: ghcr.io/myorg/rootfs:ubuntu-22.04
  probes:              # overrides class probes
    readinessProbe:
      http:
        path: /ready
        port: 8080
```

### 4.3 ImpVM

A single VM instance. References either an `ImpVMClass` directly or an
`ImpVMTemplate`. One or the other — not both.

```yaml
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: my-sandbox
spec:
  # Option A: reference a template
  templateRef:
    name: ubuntu-sandbox

  # Option B: inline (no template)
  classRef:
    name: small
  image: ghcr.io/myorg/my-app:latest   # CMD/ENTRYPOINT from OCI manifest

  networkRef:
    name: sandbox-net
  lifecycle: ephemeral                  # ephemeral | persistent

  nodeSelector:
    kubernetes.io/arch: arm64

  env:
    - name: PORT
      value: "8080"
  envFrom:
    - secretRef:
        name: my-app-secrets

  # Optional: cloud-init fallback (off by default)
  userData:
    configMapRef:
      name: my-vm-config

  # Optional probe overrides (most specific wins)
  probes:
    startupProbe:
      exec:
        command: ["my-healthcheck"]
```

**Probe inheritance chain** (most specific wins):
```
ImpVMClass.spec.probes
  └── ImpVMTemplate.spec.probes
        └── ImpVM.spec.probes
```

### 4.4 ImpNetwork

```yaml
apiVersion: imp.dev/v1alpha1
kind: ImpNetwork
metadata:
  name: sandbox-net
spec:
  subnet: 192.168.100.0/24
  gateway: 192.168.100.1
  nat:
    enabled: true
    egressInterface: eth0
  dns:
    - 1.1.1.1
  cilium:
    excludeFromIPAM: true
    masqueradeViaCilium: true
```

### 4.5 ClusterImpConfig

Cluster-scoped singleton. Controls operator-wide behaviour.

```yaml
apiVersion: imp.dev/v1alpha1
kind: ClusterImpConfig
metadata:
  name: cluster
spec:
  networking:
    cni:
      autoDetect: true
      provider: cilium-kubeproxy-free   # explicit override, skips detection
      natBackend: nftables              # nftables | iptables
    ipam:
      provider: internal                # internal | cilium-pool (phase 2)
  capacity:
    defaultFraction: 0.9               # use 90% of node compute by default
  observability:
    metrics:
      enabled: true
      port: 9090
    tracing:
      enabled: false
      endpoint: http://otel-collector:4317
```

### 4.6 ClusterImpNodeProfile

Per-node capacity overrides. If absent, `ClusterImpConfig.spec.capacity.defaultFraction` applies.

```yaml
apiVersion: imp.dev/v1alpha1
kind: ClusterImpNodeProfile
metadata:
  name: talos-node-1    # matches node name
spec:
  capacityFraction: 0.85
  maxImpVMs: 10          # hard cap regardless of compute headroom
```

---

## 5. CRD Dependency Chain

| CRD | Hard Depends On | Optional Refs | Inherited By |
|-----|-----------------|---------------|--------------|
| `ClusterImpConfig` | — | — | everything (operator-wide defaults) |
| `ImpVMClass` | — | — | `ImpVMTemplate`, `ImpVM` |
| `ImpNetwork` | — | `ClusterImpConfig` (CNI/NAT behaviour) | `ImpVMTemplate`, `ImpVM` |
| `ClusterImpNodeProfile` | k8s `Node` | — | `ImpVM` (scheduling) |
| `ImpVMTemplate` | `ImpVMClass` | `ImpNetwork`, `ConfigMap`, `Secret` | `ImpVM` |
| `ImpVM` | `ImpVMClass` **or** `ImpVMTemplate` + `ImpNetwork` | `ConfigMap`, `Secret`, `ClusterImpNodeProfile` | — |

**Probe inheritance:** `ImpVMClass` → `ImpVMTemplate` → `ImpVM`

**Compute inheritance:** `ImpVMClass.(vcpu/mem/disk)` → `ImpVMTemplate.(overrides)` → `ImpVM.(overrides)`

---

## 6. What Launches Firecracker

The node agent uses **`firecracker-go-sdk`** (official AWS Go library):

```
Node agent reconcile loop:
  ImpVM.spec.nodeName == self AND desiredState = Running
    → firecracker-go-sdk.NewMachine(ctx, cfg)
        - fork firecracker binary (from Talos extension)
        - configure via Unix socket: kernel, rootfs, TAP interface
    → machine.Start(ctx)
    → store PID + socketPath
    → poll Firecracker state API
    → update ImpVM.status
```

One Firecracker process per VM, one Unix socket per process. The node agent owns
all processes on its node.

---

## 7. OCI Image Support

VMs are defined by an OCI image, exactly like a Docker container. No cloud-init
required for standard use cases.

**Boot sequence:**

```
1. Pull OCI image layers (cached by digest via containerd on Talos node)
2. Extract layers onto base rootfs → single ext4 disk image
   (cached: same digest tuple = reuse existing ext4)
3. Read CMD/ENTRYPOINT from OCI manifest
4. Attach ext4 as block device to Firecracker
5. Start VM
6. Guest agent runs CMD → startupProbe fires → status = Running
7. If secretRef present: inject via VSOCK post-boot (never touches disk)
```

**Image caching:** the node agent maintains a local cache keyed by OCI digest
tuple. Repeat boots of the same image = ~0ms disk prep overhead.

**cloud-init** is available as an opt-in fallback via `spec.userData`. Off by default.

---

## 8. Boot Time Budget

| Scenario | Estimated Time |
|----------|----------------|
| Cold boot, first time image seen | ~1–2s |
| Warm boot, cached image | ~200–300ms |
| Snapshot restore (phase 2) | ~50–100ms |

The Firecracker VMM itself boots in ~125ms. cloud-init (if used) adds 1–5s.
For ephemeral VMs, skip cloud-init entirely — use guest agent + VSOCK.

---

## 9. Networking

Each VM gets a TAP device on the host, connected to a per-network bridge.
The node agent manages bridges + NAT rules. VSOCK handles host↔VM communication
(guest agent) with no network overhead.

### 9.1 CNI Detection

The operator detects the CNI at startup using RBAC-safe signals, in priority order:

```
1. Explicit ClusterImpConfig.spec.networking.cni.provider → use it, skip detection
2. CRD presence (cluster-scoped, minimal RBAC):
     CiliumNetworkPolicy → Cilium
     GlobalNetworkPolicy → Calico
3. DaemonSets in kube-system (may 403, handled gracefully):
     cilium-agent / kube-flannel-ds / calico-node
4. Multiple CNIs detected → Warning event, iptables fallback
5. Nothing detected → iptables fallback + Warning event
```

Detected CNI stored in operator state and emitted as a `CNIDetected` Event on startup.

### 9.2 NAT Backend by CNI

| CNI | NAT Backend |
|-----|-------------|
| Cilium (kube-proxy-free) | nftables + Cilium `ipMasqAgent` |
| Cilium (standard) | nftables + Cilium `ipMasqAgent` |
| Flannel / Calico / other | iptables MASQUERADE |
| Unknown | iptables MASQUERADE + Warning |

### 9.3 Cilium Integration

In kube-proxy-free mode, iptables rules are bypassed by eBPF. The operator
delegates masquerade to Cilium's IP masquerade agent instead.

Required `CiliumConfig` (must be applied manually — see `docs/networking/cilium.md`):

```yaml
ipMasqAgent:
  enabled: true
  config:
    nonMasqueradeCIDRs:
      - 10.0.0.0/8        # k8s pod/service CIDRs
    masqueradeCIDRs:
      - 192.168.100.0/24  # ImpNetwork subnets
```

If this configuration is missing, the operator emits a `CiliumConfigMissing` Warning
event on the `ImpNetwork` object, and links to `docs/networking/cilium.md`.

### 9.4 RBAC for CNI Detection

The operator ClusterRole ships in two profiles:

- **`full`**: includes `get/list` on DaemonSets in `kube-system`
- **`minimal`**: CRD detection only (`apiextensions.k8s.io`)

Select via Helm values. Default: `minimal`.

### 9.5 Events on ImpNetwork

| Reason | Type | Description |
|--------|------|-------------|
| `CNIDetected` | Normal | CNI identified and NAT backend selected |
| `CNIAmbiguous` | Warning | Multiple CNIs detected, explicit config required |
| `CiliumConfigMissing` | Warning | ipMasqAgent not configured for this subnet |
| `BridgeReady` | Normal | Bridge created on node |
| `IPAllocated` | Normal | IP assigned to ImpVM |
| `NATRulesApplied` | Normal | NAT rules applied |

---

## 10. Scheduling and Capacity

The operator calculates per-node VM headroom:

```
effectiveMax = min(
  floor(node.allocatable.cpu    * fraction / vm.vcpu),
  floor(node.allocatable.memory * fraction / vm.memoryMiB)
)
```

Where `fraction` defaults to `0.9` from `ClusterImpConfig`, overridden per node
via `ClusterImpNodeProfile`.

The node agent enforces the limit locally as a safety net — it refuses to start
a VM if the node is already at capacity and updates `ImpVM.status` accordingly.

NodeSelector is supported on `ImpVM.spec.nodeSelector`, same semantics as k8s Pods.

---

## 11. Talos Extension

The Firecracker binary is packaged as a Talos system extension — an OCI image
containing:

```
/usr/local/bin/firecracker    # arch-specific build
/usr/local/bin/jailer         # process isolation
```

Published to GHCR with a multi-arch manifest. KVM is already in the Talos kernel
(`kvm_intel` on amd64, `kvm` on arm64).

**Declarative setup:**

1. Build and push the extension image to GHCR
2. POST a schematic to `factory.talos.dev`:
   ```json
   {
     "customization": {
       "systemExtensions": {
         "extensions": [
           { "image": "ghcr.io/syscode-labs/talos-ext-firecracker:v1.0.0" }
         ]
       }
     }
   }
   ```
3. Factory returns a schematic ID
4. Reference the installer URL in your Talos machineconfig

Two schematics: one for `amd64` (M720q), one for `arm64` (OCI Ampere).

The node agent pod accesses `/dev/kvm` via a `hostPath` volume with a privileged
DaemonSet — standard pattern for KVM-based workloads.

---

## 12. Observability

### 12.1 Metrics (Prometheus)

Three sources, all re-exposed by the node agent on `/metrics`:

| Source | Examples |
|--------|---------|
| Firecracker FIFO | vcpu steal, memory balloon, net/block device stats |
| Guest agent (VSOCK) | cpu%, mem%, disk, open connections |
| Operator | boot latency, scheduling latency, VM counts by state, reconcile errors |

All VM metrics labeled: `impvm_name`, `namespace`, `node`, `impvmclass`.

Helm chart ships with a `ServiceMonitor` for Prometheus Operator auto-discovery
and a Grafana dashboard ConfigMap.

### 12.2 Traces (OpenTelemetry, opt-in)

```
Trace: ImpVM boot
  span: image pull (cache hit/miss)
  span: disk assembly
  span: firecracker start
  span: kernel boot
  span: startupProbe
  span: readinessProbe
```

Disabled by default. Enabled via `ClusterImpConfig.spec.observability.tracing`.

---

## 13. Phased Roadmap

### Phase 1 (v1) — Ephemeral VMs
- Talos extension (Firecracker binary)
- Node agent: Firecracker process management, TAP/bridge/NAT, OCI image support
- Operator: scheduling, CNI detection, capacity management
- CRDs: all six
- Cilium integration (nftables + ipMasqAgent)
- Prometheus metrics
- Probes via VSOCK

### Phase 2 — Persistent VMs + IPAM
- Persistent VM lifecycle (restart, rescheduling, state tracking)
- VM migration via Firecracker snapshot/restore
- Cilium IPAM delegation (`CiliumPodIPPool`)
- Warm VM pools (pre-booted snapshots for instant assign)

### Phase 3 — CI Runners + Networking
- CI runner mode (`ImpVMTemplate` with runner label + auto-registration)
- Cross-node VM networking (VXLAN overlay)
- Cilium external workloads (NetworkPolicy on VMs, Hubble visibility)

---

## 14. Testing Strategy

Given Go is a new language for this project, tests are written to be explicit and
educational — prioritising clarity over brevity.

- **Unit tests**: reconcile logic, CNI detection, probe inheritance, capacity calculation
- **Integration tests**: kubebuilder `envtest` (real API server, no cluster needed)
- **E2E tests**: Kind cluster + Firecracker mock for CI; real Talos cluster for
  hardware validation
- **Table-driven tests**: preferred pattern in Go — one test function, multiple
  named cases
