# Cilium IPAM Runbook

This runbook covers Imp networks that delegate IP allocation to Cilium.

## Scope

- Decision: use per-network pools.
- Source of truth: `ImpNetwork.spec.ipam.cilium.poolRef`.
- Allocation flow: agent resolves CIDR from `CiliumPodIPPool`, then allocates VM IPs from that CIDR.

## Prerequisites

- Cilium installed in the cluster.
- `CiliumPodIPPool` CRD present:
  - `kubectl get crd ciliumpodippools.cilium.io`
- A pool created for the target network.

## Minimal Example

```yaml
apiVersion: cilium.io/v2alpha1
kind: CiliumPodIPPool
metadata:
  name: vm-net-a
spec:
  ipv4:
    cidrs:
      - 10.77.0.0/24
    maskSize: 30
---
apiVersion: imp.dev/v1alpha1
kind: ImpNetwork
metadata:
  name: vm-net-a
  namespace: default
spec:
  subnet: 10.44.0.0/24
  ipam:
    provider: cilium
    cilium:
      poolRef: vm-net-a
```

Note: `spec.subnet` remains required for API compatibility, but VM allocation CIDR comes from the referenced Cilium pool when `provider=cilium`.

## Verification

1. Confirm pool exists:
   - `kubectl get ciliumpodippool vm-net-a -o yaml`
2. Confirm network references pool:
   - `kubectl get impnetwork vm-net-a -n default -o jsonpath='{.spec.ipam.cilium.poolRef}'`
3. Confirm agent logs are clean:
   - `kubectl -n imp-system logs ds/imp-agent`

## Failure Modes

- `CiliumPodIPPool` missing:
  - Symptom: VM start fails; agent logs include pool lookup failure.
  - Fix: create pool or correct `poolRef`.
- Pool has no `spec.cidrs`:
  - Symptom: allocation subnet resolution error.
  - Fix: configure at least one CIDR in the pool spec.
- Cilium CRD not installed:
  - Symptom: pool lookup fails with resource mismatch/not found.
  - Fix: install Cilium (or switch network IPAM provider to `internal`).

## Rollback

To stop using Cilium pool delegation for a network:

1. Patch network provider:
   - `kubectl patch impnetwork vm-net-a -n default --type merge -p '{"spec":{"ipam":{"provider":"internal"}}}'`
2. Keep or remove pool resources based on cluster policy.
