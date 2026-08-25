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

## Future: Elevated LAN Attachment

Some workloads need a physical LAN address, such as a DHCP lease from an
administrator-managed network. This is intentionally not an `ImpNetwork`
default or a normal `ImpVM` option.

The planned `ImpNetworkAttachment` resource will request an attachment to one
administrator-allowlisted network or VLAN. It can request DHCP where the
allowlisted attachment permits it.

```text
ImpVM -> ImpNetworkAttachment -> authorized network/VLAN -> bridge or CNI adapter -> DHCP
```

Before the runtime creates any host networking resource, the request must pass:

- A distinct RBAC permission for LAN attachment.
- Admission authorization for the requested allowlisted network or VLAN.
- Validation that it cannot select arbitrary host interfaces.
- Validation that it cannot change node transport-address selection.

Status, Events, logs, and traces will record the requester, authorization
result, target node, MAC address, and assigned IP or DHCP lease. Isolated
networking remains the safe default for every requester without that explicit
permission.
