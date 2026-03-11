# isolated-untrusted-builds

## What This Demonstrates

Disposable VM isolation boundary for untrusted or high-risk build/test execution.

## Why It Matters

- stronger sandbox boundary than shared container workers
- ephemeral lifecycle limits persistence after compromise
- useful for external contributions and unknown pipelines

## Manifests

- `impnetwork.yaml`: dedicated sandbox network
- `impvmclass.yaml`: sandbox worker class
- `impvmtemplate.yaml`: sandbox template
- `impvmrunnerpool.yaml`: low-idle constrained runner pool

## Prerequisites

- Secret `default/gitlab-runner-token` exists
- set `platform.serverURL` and `platform.scope` to your environment

## Apply

```sh
kubectl apply -f examples/isolated-untrusted-builds/
```

## Verify

```sh
kubectl get impvmrunnerpool sandbox-build-pool -n default
kubectl get impvms -n default -l imp.dev/runner-pool=sandbox-build-pool
```

## Cleanup

```sh
kubectl delete -f examples/isolated-untrusted-builds/
```
