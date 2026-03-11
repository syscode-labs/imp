# secure-runner-pool

Ephemeral CI runner pool with webhook-demand scaling.

## Notes
- Create `Secret default/gh-runner-token` with your platform token.
- Replace `spec.platform.scope.repo` with your repo.

## Apply
```sh
kubectl apply -f examples/secure-runner-pool/
```

## Delete
```sh
kubectl delete -f examples/secure-runner-pool/
```
