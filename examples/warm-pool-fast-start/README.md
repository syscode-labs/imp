# warm-pool-fast-start

Pre-warmed VM pool pattern for low assignment latency.

## Notes
- `snapshotRef` must point to an existing `ImpVMSnapshot` parent resource.

## Apply
```sh
kubectl apply -f examples/warm-pool-fast-start/
```

## Delete
```sh
kubectl delete -f examples/warm-pool-fast-start/
```
