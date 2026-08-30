# ADR 0001: Chart Does Not Create the Namespace

**Date:** 2026-08-30
**Status:** Accepted
**Deciders:** imp maintainers, gitops platform

## Context

Proposal to fold `imp-system` Namespace creation and Pod Security Admission (`privileged` for `enforce`/`audit`/`warn`) into `charts/imp` via `values.namespace.create` and a `templates/namespace.yaml`, removing the separate `imp-namespace` wave-0 Application in `syscode-homelab-gitops-apps`. Surveyed what other operators do.

## Survey

- `cert-manager`, `cilium`, `kyverno`, `kube-prometheus-stack`, `external-secrets`, `flux2` chart — all **namespace-free**; install via `helm --create-namespace` or `argocd.argoproj.io/syncOptions: CreateNamespace=true`. Documented prerequisite, not manifested.
- CLI-bootstrapped tools (`flux bootstrap`, `istioctl`, `linkerd`) create the namespace in the *tool*, not the chart.
- PSA `privileged` for `kube-system`-adjacent workloads is **documented** ("label the namespace") — e.g., Istio CNI/Linkerd, Cilium — not applied by charts. PSA level is a cluster-admin policy, not the vendor's.
- Minority with `namespace.create` toggle (some Bitnami-family, kiali) default `false` for the same reasons.
- GitOps-native pattern: management repo owns cluster policy as a wave-0 app — exactly our `imp-namespace` (`clusters/unraid-lab/apps/imp-namespace/manifests/namespace.yaml`).

## Decision

`charts/imp` **stays namespace-free**. It uses `.Release.Namespace` and never creates `kind: Namespace` or sets `pod-security.kubernetes.io/*` labels. Operators document the prerequisite in `README.md` and `charts/imp/README.md`; the GitOps repo owns the namespace (`imp-namespace` app, `CreateNamespace=true`, `sync-wave: "0"`).

## Consequences

- One extra Application in the GitOps repo (`imp-namespace`), but cascade-delete isolation is preserved: deleting the `imp` Application does **not** delete `imp-system` (and everything in it, including resources from other apps). A chart-owned namespace + Argo finalizer would.
- Namespace creation remains portable to RBAC-restricted installs (`helm --namespace` with limited SA) and avoids Helm best-practice violation (don't create the release namespace).
- PSA `privileged` stays an explicit cluster-admin choice, not a silent chart default — compatible with PSS-restricted clusters and Kyverno/PSA enforcement.
- OCI consumers (`helm show readme`) get the prerequisite from `charts/imp/README.md`, which links to this ADR.
