# imp-sandbox

Optional AI-agent sandbox add-on for [Imp](../../README.md). Installing this
chart is what activates the sandbox feature; base Imp deployments are
completely unaffected when it is absent.

## What it installs

- `ImpSandbox` CRD (`sandbox.imp.dev/v1alpha1`) in `crds/`
- The `imp-sandbox` controller Deployment (own manager, own ServiceAccount,
  scoped RBAC) — reconciles sandboxes into base `ImpVM`/`ImpNetwork` resources
- A validating webhook enforcing tenancy rules (cluster floor, fail-closed
  `hard` tenancy without Cilium)
- Nothing else: no privileged pods, no host mounts, no changes to the base chart

## Requirements

- An installed `imp` release (the add-on reconciles base resources)
- cert-manager (webhook TLS), matching the base chart's requirement
- Cilium is required only to create `tenancy: hard` sandboxes; admission fails
  closed without it

## Install

```sh
kubectl create namespace imp-sandbox-system --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install imp-sandbox ./charts/imp-sandbox -n imp-sandbox-system
```

## Uninstall

```sh
helm uninstall imp-sandbox -n imp-sandbox-system
kubectl delete crd impsandboxes.sandbox.imp.dev   # optional; GC removes owned VMs first
```

Uninstalling returns the cluster to plain Imp. Sandbox-owned `ImpVM`s are
deleted via owner references before the CRD is removed.

## Creating a sandbox

```yaml
apiVersion: sandbox.imp.dev/v1alpha1
kind: ImpSandbox
metadata:
  name: agent-1
  namespace: default          # tenant boundary: objects land here
spec:
  templateRef:
    name: my-agent-template    # an existing ImpVMTemplate, or use classRef+image
  tenancy: standard            # or "hard" (requires Cilium)
  expireAfter: 4h              # optional TTL, mirrors ImpVM semantics
```

The generated `ImpVM` (`agent-1`) and `ImpNetwork` (`agent-1-net`, unless
`spec.networkRef` is set) appear in the same namespace, labeled
`sandbox.imp.dev/owner: agent-1`.

## Tenancy model

| Tier | Baseline deny (always on) | Inter-sandbox isolation | Egress policy |
|------|---------------------------|-------------------------|---------------|
| `standard` (default) | cloud metadata + cluster-internal ranges | no | none |
| `hard` | cloud metadata + cluster-internal ranges | yes | enforced (Cilium) |

Set a cluster-wide minimum via `ClusterImpConfig.spec.sandbox.floorTenancy`.

## Values

See `values.yaml`. Everything is namespaced under `sandbox.` (deployment),
`webhook.` and `rbac.`.
