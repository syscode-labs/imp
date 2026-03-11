# batch-media-workers

Queue-driven batch worker pool with burst scaling.

## Notes
- Create `Secret default/forgejo-runner-token`.
- Replace `platform.serverURL` and scope values.

## Apply
```sh
kubectl apply -f examples/batch-media-workers/
```

## Delete
```sh
kubectl delete -f examples/batch-media-workers/
```
