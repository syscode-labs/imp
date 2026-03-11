# compliance-segmented-workloads

## What This Demonstrates

Regulated workload placement on dedicated nodes with dedicated network segmentation.

## Why It Matters

- explicit placement for sensitive processing
- easier audit story (where workload can run)
- foundation for stricter org-level controls and policies

## Manifests

- `impnetwork.yaml`: segmented network for regulated workloads
- `impvmclass.yaml`: regulated compute profile
- `impvm.yaml`: workload with node selector constraints

## Prerequisites

- target nodes labeled `compliance.imp.dev/zone=regulated`

## Apply

```sh
kubectl apply -f examples/compliance-segmented-workloads/
```

## Verify

```sh
kubectl get impvm regulated-workload-01 -n default -o jsonpath='{.spec.nodeName}{"\n"}'
kubectl describe impvm regulated-workload-01 -n default
```

## Cleanup

```sh
kubectl delete -f examples/compliance-segmented-workloads/
```
