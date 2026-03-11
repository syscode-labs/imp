# legacy-app-lift-and-shift

## What This Demonstrates

A persistent VM pattern for workloads not yet containerized but still managed declaratively.

## Why It Matters

- supports phased migration from legacy runtime models
- keeps Kubernetes-native control plane and status/conditions
- avoids immediate full replatforming pressure

## Manifests

- `impvmclass.yaml`: persistent service class
- `impvm.yaml`: long-running legacy service VM

## Apply

```sh
kubectl apply -f examples/legacy-app-lift-and-shift/
```

## Verify

```sh
kubectl get impvm legacy-service-vm -n default
kubectl describe impvm legacy-service-vm -n default
```

## Cleanup

```sh
kubectl delete -f examples/legacy-app-lift-and-shift/
```
