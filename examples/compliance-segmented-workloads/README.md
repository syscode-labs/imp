# compliance-segmented-workloads

Sensitive workload pinned to dedicated nodes and network segment.

## Notes
- Requires nodes labeled `compliance.imp.dev/zone=regulated`.

## Apply
```sh
kubectl apply -f examples/compliance-segmented-workloads/
```

## Delete
```sh
kubectl delete -f examples/compliance-segmented-workloads/
```
