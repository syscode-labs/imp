# warm-pool-fast-start

## What This Demonstrates

A warm pool that keeps pre-booted VMs ready from a snapshot source for faster assignment.

## Why It Matters

- reduces cold-start latency for burst workloads
- useful when startup path includes expensive initialization
- supports low-latency execution platforms

## Manifests

- `impvmclass.yaml`: compute profile for warm members
- `impvmtemplate.yaml`: VM template used to create pool members
- `impwarmpool.yaml`: desired pool size and snapshot source

## Prerequisites

- `spec.snapshotRef` points to an existing `ImpVMSnapshot` parent resource
- a base snapshot has been elected for that snapshot parent

## Apply

```sh
kubectl apply -f examples/warm-pool-fast-start/
```

## Verify

```sh
kubectl get impwarmpool fast-start-pool -n default
kubectl get impvms -n default -l imp.dev/warm-pool=fast-start-pool
```

## Cleanup

```sh
kubectl delete -f examples/warm-pool-fast-start/
```
