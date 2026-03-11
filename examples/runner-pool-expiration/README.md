# runner-pool-expiration

## What This Demonstrates

A complete expiration chain for runner VMs:

- template-level default (`2h`)
- runner-pool override (`45m`)
- VM-level override (optional, highest precedence)

## Why It Matters

- caps maximum runtime for stuck/long-lived runners
- bounds cost and security exposure for CI workers
- keeps burst pools self-cleaning even when jobs misbehave

## Manifests

- `impnetwork.yaml`: isolated CI runner network
- `impvmclass.yaml`: compute profile for runners
- `impvmtemplate.yaml`: base VM template with `expireAfter: 2h`
- `impvmrunnerpool.yaml`: pool override with `expireAfter: 45m`

## Precedence

1. `ImpVM.spec.expireAfter`
2. `ImpVMRunnerPool.spec.expireAfter`
3. `ImpVMTemplate.spec.expireAfter`
4. disabled (`0` or omitted)

## Prerequisites

- Secret `default/gh-runner-token` exists
- set `spec.platform.scope.repo` to your repository

## Apply

```sh
kubectl apply -f examples/runner-pool-expiration/
```

## Verify

```sh
kubectl get impvmrunnerpool ci-runner-expiring -n default
kubectl get impvms -n default -l imp.dev/runner-pool=ci-runner-expiring
kubectl get impvm -n default -l imp.dev/runner-pool=ci-runner-expiring -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.runningAt}{"\t"}{.status.expiresAt}{"\n"}{end}'
```

## Cleanup

```sh
kubectl delete -f examples/runner-pool-expiration/
```
