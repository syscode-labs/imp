# secure-runner-pool

## What This Demonstrates

A CI runner pool pattern where VMs are created on demand from webhook signals and terminated after jobs. This is the core "burst-to-zero" model for self-hosted runner capacity.

## Why It Matters

- avoids always-on idle runners
- limits blast radius with ephemeral VMs
- keeps baseline cost close to zero when idle

## Manifests

- `impnetwork.yaml`: isolated network for runner VMs
- `impvmclass.yaml`: compute profile for runner instances
- `impvmtemplate.yaml`: reusable runner VM blueprint
- `impvmrunnerpool.yaml`: demand-driven runner pool settings

## Prerequisites

- Secret `default/gh-runner-token` exists
- `spec.platform.scope.repo` points to your real repo

## Apply

```sh
kubectl apply -f examples/secure-runner-pool/
```

## Verify

```sh
kubectl get impvmrunnerpool ci-runner-pool -n default
kubectl get impvms -n default -l imp.dev/runner-pool=ci-runner-pool
```

## Cleanup

```sh
kubectl delete -f examples/secure-runner-pool/
```
