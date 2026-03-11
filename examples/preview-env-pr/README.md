# preview-env-pr

## What This Demonstrates

A simple per-PR preview environment pattern using one ephemeral `ImpVM` with health checks.

## Why It Matters

- gives isolated preview runtime per change
- easy to attach to PR lifecycle automation
- no manual environment cleanup when using ephemeral lifecycle

## Manifests

- `impvmclass.yaml`: small preview compute class
- `impvm.yaml`: one preview VM instance

## Customization

- change VM name to include your PR identifier
- replace image with preview build artifact image
- tune `httpCheck` path/port to your app

## Apply

```sh
kubectl apply -f examples/preview-env-pr/
```

## Verify

```sh
kubectl get impvm preview-pr-1234 -n default -o wide
kubectl describe impvm preview-pr-1234 -n default
```

## Cleanup

```sh
kubectl delete -f examples/preview-env-pr/
```
