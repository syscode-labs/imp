# github-scaling-explicit

## What This Demonstrates

GitHub-first explicit scaling on `ImpVMRunnerPool` using `spec.scaling` mode controls.

## Why It Matters

- no implicit capacity defaults for GitHub scaling pools
- explicit hard ceilings and pace controls
- clear mode selection (`webhook`, `polling`, `hybrid`) in `spec.scaling.mode`

## Manifests

- `impnetwork.yaml`: isolated network for runner VMs
- `impvmclass.yaml`: compute profile
- `impvmtemplate.yaml`: runner VM template
- `impvmrunnerpool.yaml`: explicit scaling configuration

## Important

Set these scaling fields explicitly:

- `mode`
- `minIdle`
- `maxConcurrent`
- `scaleUpStep`
- `cooldownSeconds`

GitHub (`platform.type=github-actions`) is supported for this explicit scaling mode in this first version.

## Prerequisites

- Secret `default/gh-runner-token` exists
- Secret `default/gh-webhook-secret` exists
- set `platform.scope.repo` to your repository

## Apply

```sh
kubectl apply -f examples/github-scaling-explicit/
```

## Verify

```sh
kubectl get impvmrunnerpool ci-runner-explicit -n default -o yaml
kubectl get impvms -n default -l imp.dev/runner-pool=ci-runner-explicit
```

## Cleanup

```sh
kubectl delete -f examples/github-scaling-explicit/
```
