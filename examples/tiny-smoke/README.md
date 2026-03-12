# tiny-smoke

## What This Demonstrates

A constrained-footprint smoke path for validating:
- classRef-based VM boot
- VM-to-VM east-west traffic on one `ImpNetwork`

This is intended for tiny nodes and one-off validation, not production sizing.

## Manifests

- `impvmclass.yaml`: tiny compute profile
- `impnetwork.yaml`: isolated smoke network
- `vm-server.yaml`: long-running server VM
- `vm-client.yaml`: long-running client VM

## Prerequisites

- `imp` is installed and healthy.
- Guest agent is enabled (default).
- Agent can run Firecracker on at least one node.
- `kubectl` and `curl` are installed on your workstation.

## Run Smoke Validation

```sh
bash examples/tiny-smoke/run.sh
```

What `run.sh` does:
1. Applies class, network, and two VMs.
2. Waits for both VMs to reach `Running`.
3. Finds the `imp-agent` pod on the client VM node.
4. Port-forwards the agent API.
5. Executes a guest-agent command from client VM to server VM (`ping`) over the ImpNetwork.
6. Fails unless guest-exec returns exit code `0`.

## Cleanup

```sh
kubectl delete -f examples/tiny-smoke/
```
