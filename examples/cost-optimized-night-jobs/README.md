# cost-optimized-night-jobs

## What This Demonstrates

A scale-from-zero runner pool profile for periodic/nightly job bursts.

## Why It Matters

- minimizes idle baseline spend
- still supports rapid burst for scheduled windows
- hard caps protect cluster capacity

## Manifests

- `impvmclass.yaml`: nightly job class
- `impvmtemplate.yaml`: job template
- `impvmrunnerpool.yaml`: zero-idle burst policy

## Prerequisites

- Secret `default/nightly-gh-token` exists
- set `platform.scope.repo` to your target repository

## Apply

```sh
kubectl apply -f examples/cost-optimized-night-jobs/
```

## Verify

```sh
kubectl get impvmrunnerpool nightly-job-pool -n default
kubectl get impvms -n default -l imp.dev/runner-pool=nightly-job-pool
```

## Cleanup

```sh
kubectl delete -f examples/cost-optimized-night-jobs/
```
