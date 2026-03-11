# batch-media-workers

## What This Demonstrates

Elastic worker fleet for queue-like batch workloads using `ImpVMRunnerPool` scaling.

## Why It Matters

- scales out for spikes, scales in when queue drains
- isolates heavy media/transcode tasks from core services
- enforces upper concurrency limits

## Manifests

- `impnetwork.yaml`: worker egress network
- `impvmclass.yaml`: larger worker compute profile
- `impvmtemplate.yaml`: worker VM template
- `impvmrunnerpool.yaml`: polling-driven burst policy

## Prerequisites

- Secret `default/forgejo-runner-token` exists
- set `platform.serverURL` and `platform.scope` to your environment

## Apply

```sh
kubectl apply -f examples/batch-media-workers/
```

## Verify

```sh
kubectl get impvmrunnerpool media-worker-pool -n default
kubectl get impvms -n default -l imp.dev/runner-pool=media-worker-pool
```

## Cleanup

```sh
kubectl delete -f examples/batch-media-workers/
```
