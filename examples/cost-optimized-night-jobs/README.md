# cost-optimized-night-jobs

Scale-from-zero runner pool for nightly demand spikes.

## Notes
- Create `Secret default/nightly-gh-token`.
- Replace `platform.scope.repo` with your repo.

## Apply
```sh
kubectl apply -f examples/cost-optimized-night-jobs/
```

## Delete
```sh
kubectl delete -f examples/cost-optimized-night-jobs/
```
