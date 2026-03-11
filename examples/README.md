# Examples

This directory contains realistic teaser deployments for common `imp` use cases.

Each example is designed to be:
- small enough to apply quickly
- representative of a real operational pattern
- easy to adapt for your own environment

## How To Use These Examples

1. Read the example README first.
2. Update any placeholder values (`your-org/your-repo`, token secret names, URLs).
3. Apply the manifests in that example directory.
4. Verify resources using the suggested `kubectl` commands.
5. Delete when finished.

## Included Teasers

- `secure-runner-pool`: Ephemeral CI runners that scale from webhook demand
- `preview-env-pr`: Per-PR preview environment VM pattern
- `warm-pool-fast-start`: Pre-warmed pool for low-latency VM assignment
- `multi-tenant-isolation`: Team isolation via namespaces and networks
- `edge-proxy-pop`: Node-pinned edge placement using selectors/tolerations
- `batch-media-workers`: Burst worker pool for queue-driven batch workloads
- `legacy-app-lift-and-shift`: Persistent VM for non-containerized workloads
- `isolated-untrusted-builds`: Disposable VM isolation boundary for risky builds
- `compliance-segmented-workloads`: Sensitive workloads pinned to dedicated nodes
- `cost-optimized-night-jobs`: Scale-from-zero capacity for periodic spikes

## Notes

- These examples intentionally use `ghcr.io/syscode-labs/test:latest` as a neutral placeholder image.
- Secrets referenced by runner pool examples must be created separately.
- GPU passthrough is not supported in `imp`/Firecracker here; examples focus on CPU/memory/storage-isolated workloads.
