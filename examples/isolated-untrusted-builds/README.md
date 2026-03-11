# isolated-untrusted-builds

Disposable VMs for running untrusted build/test jobs.

## Notes
- Create `Secret default/gitlab-runner-token`.
- Replace `platform.serverURL` and scope values.

## Apply
```sh
kubectl apply -f examples/isolated-untrusted-builds/
```

## Delete
```sh
kubectl delete -f examples/isolated-untrusted-builds/
```
