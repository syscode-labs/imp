# edge-proxy-pop

## What This Demonstrates

Placement control for edge-style workloads by pinning VM scheduling to selected nodes.

## Why It Matters

- deterministic placement near ingress points
- isolates edge traffic workloads from general compute nodes
- practical for cache/proxy POP-like topologies

## Manifests

- `impvmclass.yaml`: edge proxy compute class
- `impvmtemplate.yaml`: reusable edge proxy template
- `impvm.yaml`: node-selected, toleration-aware edge VM

## Prerequisites

- target nodes labeled `topology.imp.dev/role=edge`
- matching taints/tolerations configured if using tainted edge nodes

## Apply

```sh
kubectl apply -f examples/edge-proxy-pop/
```

## Verify

```sh
kubectl get impvm edge-proxy-01 -n default -o jsonpath='{.spec.nodeName}{"\n"}'
```

## Cleanup

```sh
kubectl delete -f examples/edge-proxy-pop/
```
