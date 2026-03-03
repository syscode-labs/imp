# Helm Chart Design

**Goal:** Package Imp for production deployment via two Helm charts — `imp-crds` and `imp` — compatible with Flux, ArgoCD, and plain Helm.

---

## Chart Layout

Two separate charts, following the cert-manager pattern:

```
charts/
├── imp-crds/                        # install once, upgrade independently
│   ├── Chart.yaml
│   └── templates/
│       ├── impvm.yaml
│       ├── impvmclass.yaml
│       ├── impvmtemplate.yaml
│       ├── impnetwork.yaml
│       ├── clusterimpconfig.yaml
│       └── clusterimpnodeprofile.yaml
└── imp/
    ├── Chart.yaml
    ├── values.yaml
    └── templates/
        ├── _helpers.tpl
        ├── operator/
        │   ├── deployment.yaml
        │   ├── serviceaccount.yaml
        │   ├── clusterrole.yaml        # minimal CNI RBAC (always created)
        │   ├── clusterrole-full.yaml   # full CNI RBAC (opt-in)
        │   ├── clusterrolebinding.yaml
        │   └── service.yaml            # port 9443 (webhook) + 8080 (metrics)
        ├── agent/
        │   ├── daemonset.yaml
        │   ├── serviceaccount.yaml
        │   ├── clusterrole.yaml
        │   └── clusterrolebinding.yaml
        └── webhook/
            ├── certificate.yaml
            ├── issuer.yaml
            ├── validatingwebhook.yaml
            └── mutatingwebhook.yaml
```

`imp-crds` CRDs carry `helm.sh/resource-policy: keep` — they survive `helm uninstall`. The `imp` chart has no Helm dependency on `imp-crds`; install order is managed by the GitOps tool.

---

## values.yaml

```yaml
nameOverride: ""
fullnameOverride: ""
imagePullSecrets: []

operator:
  image:
    repository: ghcr.io/syscode-labs/imp-operator
    tag: ""             # defaults to .Chart.AppVersion
    pullPolicy: IfNotPresent
  replicaCount: 1
  resources:
    requests:
      cpu: 100m
      memory: 64Mi
    limits:
      memory: 128Mi
  nodeSelector: {}
  rbac:
    cniDetection: minimal   # minimal | full

agent:
  image:
    repository: ghcr.io/syscode-labs/imp-agent
    tag: ""
    pullPolicy: IfNotPresent
  resources:
    requests:
      cpu: 100m
      memory: 64Mi
    limits:
      memory: 128Mi
  nodeSelector: {}
  env:
    kernelPath: ""    # required — FC_KERNEL; helm install fails if not set
                      # kernelArgs is intentionally absent: the binary defaults to
                      # "console=ttyS0 reboot=k panic=1 pci=off"; only override
                      # via a custom values file if you have a specific reason to
  hostPaths:
    socketDir:
      enabled: false
      path: /run/imp/sockets
    imageCache:
      enabled: false
      path: /var/lib/imp/images

webhook:
  certManager:
    enabled: true
    issuerRef: {}     # optional: reference an existing Issuer instead of creating one
```

### Notes

- `agent.env.kernelPath` maps to `FC_KERNEL`. The template uses Helm's `required` function — `helm install` fails immediately if not set. This is intentional: there is no safe default for the kernel path.
- `agent.env.kernelArgs` is **not** a values key. The binary defaults to `console=ttyS0 reboot=k panic=1 pci=off` when `FC_KERNEL_ARGS` is absent. Override only via a custom values file if you have a specific reason.
- `agent.hostPaths`: when disabled (default), socket dir and image cache use `emptyDir`. Enable for production to persist the OCI image cache across agent rollouts and socket state across restarts.

---

## RBAC

### Operator ClusterRole (always created)

| API Group | Resources | Verbs |
|-----------|-----------|-------|
| `imp.dev` | all 6 CRDs | get, list, watch, update, patch |
| `imp.dev` | all 6 CRDs/status | get, update, patch |
| `""` (core) | nodes | get, list, watch |
| `""` (core) | events | create, patch |
| `coordination.k8s.io` | leases | get, list, watch, create, update, patch |
| `apiextensions.k8s.io` | customresourcedefinitions | get, list |

### Operator ClusterRole — full CNI detection (opt-in)

Created only when `operator.rbac.cniDetection: full`. Grants `apps: get/list DaemonSets` cluster-wide (Kubernetes does not support namespace-scoped `resourceNames` for list).

### Agent ClusterRole

| API Group | Resources | Verbs |
|-----------|-----------|-------|
| `imp.dev` | impvms | get, list, watch, update, patch |
| `imp.dev` | impvms/status | get, update, patch |
| `imp.dev` | impvmclasses | get |
| `imp.dev` | impnetworks | get |
| `""` (core) | events | create, patch |

The agent only reconciles VMs where `spec.nodeName == NODE_NAME` (enforced in Go, not RBAC — same pattern as kubelet).

---

## Webhook + cert-manager

```
Issuer (self-signed, namespace-scoped)
  └── Certificate
        ├── secretName: imp-webhook-tls
        ├── dnsNames:
        │     - imp-operator-webhook.{{ .Release.Namespace }}.svc
        │     - imp-operator-webhook.{{ .Release.Namespace }}.svc.cluster.local
        ├── duration: 8760h (1 year)
        └── renewBefore: 720h (30 days)
```

`ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration` carry:
```yaml
annotations:
  cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/imp-webhook-tls
```

cert-manager's `cainjector` populates `caBundle` automatically — no manual rotation.

When `webhook.certManager.issuerRef` is set, the chart skips creating the `Issuer` and references the provided one instead (for clusters with a shared PKI).

---

## GitOps Install

### Flux

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: imp-crds
spec:
  chart:
    spec:
      chart: imp-crds
  upgrade:
    crds: CreateReplace
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: imp
spec:
  dependsOn:
    - name: imp-crds
  chart:
    spec:
      chart: imp
  values:
    agent:
      env:
        kernelPath: /usr/local/bin/firecracker
```

### ArgoCD

Two Applications: `imp-crds` with `syncWave: "0"`, `imp` with `syncWave: "1"`.

### Plain Helm

```bash
helm install imp-crds charts/imp-crds
helm install imp charts/imp --set agent.env.kernelPath=/usr/local/bin/firecracker
```
