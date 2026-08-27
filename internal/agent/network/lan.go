package network

import (
	"context"
	"strconv"
)

// LANAttacher provisions host networking for access-mode LAN/VLAN attachments.
// It is a separate, optional seam next to NetManager so the capability can be
// absent (e.g. behind a runtime socket client that does not implement it yet)
// without changing the NetManager contract or its wire protocol.
//
// Invariants (enforced by implementations):
//   - The parent interface is never deleted, reconfigured, or enslaved for
//     tagged attachments; only an Imp-created VLAN subinterface is used.
//   - Untagged attachments require the bound parent to be an existing bridge
//     managed by the administrator; Imp never converts a physical interface
//     into a bridge itself, because that would move node transport addresses.
//   - No IP address, NAT rule, or FDB entry is ever created for LAN bridges;
//     addressing belongs to the physical network (static or guest DHCP).
//   - All operations are idempotent.
type LANAttacher interface {
	// EnsureLANBridge prepares the bridge a VM's TAP attaches to:
	//   - vlanID > 0: ensures the 802.1Q subinterface VLANIfaceName(parentIface,
	//     vlanID) over parentIface, creates bridgeName when missing (without any
	//     address), and enslaves the subinterface to it.
	//   - vlanID == 0: validates parentIface is an existing bridge; bridgeName
	//     is ignored and VMs attach to parentIface directly.
	EnsureLANBridge(ctx context.Context, bridgeName, parentIface string, vlanID int32) error

	// TeardownLANBridgeIfUnused removes Imp-created resources for a tagged
	// attachment once no TAP ports remain: bridgeName first, then the VLAN
	// subinterface. A no-op while ports remain, for untagged attachments, or
	// when resources are already gone. Never touches parentIface itself.
	TeardownLANBridgeIfUnused(ctx context.Context, bridgeName, parentIface string, vlanID int32) error
}

// StubLANAttacher records calls for tests without touching the host.
type StubLANAttacher struct {
	EnsureCalls   []string // "bridge|parent|vlanID"
	TeardownCalls []string // "bridge|parent|vlanID"
	EnsureErr     error
	TeardownErr   error
}

func (s *StubLANAttacher) EnsureLANBridge(_ context.Context, bridgeName, parentIface string, vlanID int32) error {
	s.EnsureCalls = append(s.EnsureCalls, lanCallKey(bridgeName, parentIface, vlanID))
	return s.EnsureErr
}

func (s *StubLANAttacher) TeardownLANBridgeIfUnused(_ context.Context, bridgeName, parentIface string, vlanID int32) error {
	s.TeardownCalls = append(s.TeardownCalls, lanCallKey(bridgeName, parentIface, vlanID))
	return s.TeardownErr
}

func lanCallKey(bridgeName, parentIface string, vlanID int32) string {
	return bridgeName + "|" + parentIface + "|" + strconv.FormatInt(int64(vlanID), 10)
}
