# LAN/VLAN Attachment

## What This Demonstrates

Attaching a microVM to a physical, administrator-allowlisted VLAN in access
mode — the VM leaves Imp's isolated overlay and gets an identity on the
physical network (static address or DHCP lease).

This is the elevated networking path. Isolated `ImpNetwork`s remain the
default and are unchanged.

## Manifests

- `admin-allowlist.yaml`: cluster-wide attachment definition (VLAN 100) plus
  the per-node parent-interface binding (`enp3s0` on `worker-a`)
- `vm-attachment.yaml`: the user-facing `ImpNetworkAttachment` requesting
  attachment for one VM

## Prerequisites

- `imp` is installed with admission webhooks enabled.
- The applying user holds the distinct `impnetworkattachments` create
  permission; default ImpVM/ImpNetwork roles do not.
- For untagged definitions (`vlanID: 0`), the bound parent must be an existing
  bridge managed by the administrator.
- The referenced VM has no `spec.networkRef` (a VM uses either an ImpNetwork
  or an attachment as its single NIC).

## Run

```sh
kubectl apply -f examples/lan-vlan-attachment/admin-allowlist.yaml
kubectl apply -f examples/lan-vlan-attachment/vm-attachment.yaml

kubectl get impnetworkattachment tiny-vm-vlan100 -o yaml   # status.node, macAddress, phase
```

The controller authorizes once the referenced VM is scheduled to a node whose
profile carries the binding; the node agent then bridges the VM's TAP onto the
VLAN subinterface (`enp3s0.100`). No NAT, no Imp IPAM, no VXLAN involvement.

## Safety Model

- Users reference allowlisted definitions by name only; host interfaces are
  never user-selectable.
- Deletion is refused while the VM runs so teardown always happens via VM stop,
  which removes Imp-created bridge/subinterface resources but never the parent.
