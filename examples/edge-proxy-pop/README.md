# edge-proxy-pop

Node-pinned edge proxy workload using node selector + toleration.

## Notes
- Requires edge nodes labeled `topology.imp.dev/role=edge`.

## Apply
```sh
kubectl apply -f examples/edge-proxy-pop/
```

## Delete
```sh
kubectl delete -f examples/edge-proxy-pop/
```
