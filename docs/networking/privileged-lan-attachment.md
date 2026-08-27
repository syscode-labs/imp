# Isolated and Privileged VM Networking

## Default: Isolated Networks

`ImpNetwork` uses an isolated guest CIDR by default. The runtime may create
TAP, bridge, NAT, VXLAN, and FDB resources to provide that network, but guest
addresses are not node addresses.

```text
Node transport: 192.168.122.x
Pod and Service CIDRs: cluster-owned ranges
Imp guest CIDR: isolated and non-overlapping range
```

The VTEP underlay address comes from a stable node-transport source. It must
not be derived from Kubernetes `status.hostIP`, because a host-networked runtime
can create guest bridge addresses that would make that value mutable. Planned
admission validation will reject an isolated guest CIDR overlapping a configured
node, Pod, or Service CIDR.

Configure the stable address on the profile whose name matches the Kubernetes
node name:

```yaml
apiVersion: imp.dev/v1alpha1
kind: ClusterImpNodeProfile
metadata:
  name: worker-a
spec:
  vtepIP: 192.168.122.10
```

Without `spec.vtepIP`, the node still supports isolated local VMs, but Imp
does not enable cross-node VXLAN for that node.

## Elevated LAN/VLAN Attachment

Some workloads need a physical LAN address, such as a DHCP lease from an
administrator-managed network. This is intentionally not an `ImpNetwork`
default or a normal `ImpVM` option.

The `ImpNetworkAttachment` resource requests an attachment of one ImpVM to one
administrator-allowlisted network or VLAN in access mode. It can request DHCP
where the allowlisted attachment permits it.

```text
ImpVM -> ImpNetworkAttachment -> authorized network/VLAN -> bridge adapter -> static IP or DHCP
```

### Definitions and Bindings

- Attachment definitions are declared cluster-wide in
  `ClusterImpConfig.spec.networking.lanAttachments[]`: name, VLAN ID
  (`0` = untagged, `1–4094` = tagged), subnet CIDR, DHCP permission, optional
  subject allowlist (`user:…` / `group:…` entries).
- Physical bindings live in `ClusterImpNodeProfile.spec.lanBindings[]`,
  mapping a definition to the parent interface on that node. For untagged
  attachments the parent must be an existing administrator-managed bridge;
  for tagged attachments Imp creates the `802.1Q` subinterface
  (`<parent>.<vlanID>`) itself.
- Users reference definitions by name only. Host interfaces are never
  user-selectable, and node transport-address selection is never altered.
- A VM uses either an `ImpNetwork` or an attachment as its single NIC; a VM
  with `spec.networkRef` cannot be attached.

### Authorization Flow

Before the runtime creates any host networking resource, the request must pass:

- A distinct RBAC permission: creating `impnetworkattachments`. Default
  `ImpVM` and `ImpNetwork` permissions do not grant it.
- Admission validation of allowlist membership, DHCP policy, static IP/subnet
  consistency, subject restrictions, and spec immutability after creation.
- Controller authorization against the referenced VM's assigned node binding;
  definitions deleted later downgrade authorized attachments on the next pass.

Status, Events, and logs record the requester, authorization result, target
node, MAC address, and assigned static IP or DHCP lease state.

### Teardown Invariants

Deletion is refused while the referenced VM runs so teardown always flows
through VM stop. Stop removes only Imp-created resources — the TAP, and for
tagged attachments the Imp bridge plus VLAN subinterface once no TAPs remain.
The parent interface is never deleted or reconfigured. Untagged attachments
bridge onto administrator-owned bridges that Imp leaves untouched.

A runnable walkthrough lives in `examples/lan-vlan-attachment/`.
