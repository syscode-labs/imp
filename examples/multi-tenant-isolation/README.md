# multi-tenant-isolation

## What This Demonstrates

A two-tenant isolation baseline using separate namespaces, networks, and VM classes.

## Why It Matters

- clear workload ownership boundaries
- easy per-team policy/quota extensions
- good default shape for internal platform multi-tenancy

## Manifests

- `team-a-*`: namespace, network, class, and VM for Team A
- `team-b-*`: namespace, network, class, and VM for Team B

## Apply

```sh
kubectl apply -f examples/multi-tenant-isolation/
```

## Verify

```sh
kubectl get ns team-a team-b
kubectl get impnetwork -A | grep team-
kubectl get impvm -A | grep team-
```

## Cleanup

```sh
kubectl delete -f examples/multi-tenant-isolation/
```
