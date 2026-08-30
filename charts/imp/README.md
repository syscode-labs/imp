# imp Helm Chart

Imp — Firecracker microVM operator for Kubernetes.

## Install

```sh
kubectl create namespace imp-system --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace imp-system \
  pod-security.kubernetes.io/enforce=privileged \
  pod-security.kubernetes.io/audit=privileged \
  pod-security.kubernetes.io/warn=privileged \
  --overwrite
helm upgrade --install imp oci://ghcr.io/syscode-labs/charts/imp --version 0.9.0 -n imp-system --create-namespace
```

Requires at least one Ready node labeled `imp/enabled=true` (Talos/Omni: machine config patch). See the [root README](../../README.md#quickstart) for full quickstart.

## Namespace & Pod Security

The agent requires `privileged` Pod Security Admission. Prepare the target namespace before install — the chart **intentionally does not** create the namespace or set PSA labels:

- `pod-security.kubernetes.io/enforce=privileged`
- `pod-security.kubernetes.io/audit=privileged`
- `pod-security.kubernetes.io/warn=privileged`

This matches `cert-manager`, `cilium`, `kyverno`, and `flux2` — see [ADR 0001](../../docs/adr/0001-chart-does-not-create-namespace.md). In GitOps (Argo CD), manage the namespace as a separate wave-0 Application (`imp-system`).

Keep `imp-system` as the only `privileged` namespace; all others remain `restricted`.

## Values Highlights

- `priorityClass.create` — ships `imp-critical`/`imp-high`
- `kvm.preflight.enabled` — blocks install until `imp/enabled=true` nodes pass `/dev/kvm` probe
- `agent.nodeSelector` / `kvm.preflight.nodeSelector` default to `imp/enabled=true`
