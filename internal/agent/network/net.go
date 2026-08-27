package network

import (
	"context"
	"sort"
	"strconv"
)

// FDBEntry is a MAC→VTEP mapping for the VXLAN FDB.
type FDBEntry struct {
	MAC   string
	DstIP string
}

// NetworkInfo holds all networking state for a running VM.
type NetworkInfo struct {
	TAPName         string   // e.g. "imptap-a1b2c3d4"
	BridgeName      string   // e.g. "impbr-e5f6a7b8"
	MACAddr         string   // e.g. "02:ab:cd:ef:01:23"
	IP              string   // VM's assigned IP, e.g. "192.168.100.2"
	PrefixLen       int      // subnet prefix length, e.g. 24
	Gateway         string   // bridge/gateway IP, e.g. "192.168.100.1"
	DNS             []string // nameservers injected into VM
	Subnet          string   // e.g. "192.168.100.0/24"
	NetworkKey      string   // e.g. "default/mynet" — used by Allocator.Release
	ClaimHolder     string   // VM key recorded in the Kubernetes Lease claim
	NATEnabled      bool     // true when NAT was enabled for this network
	EgressInterface string   // egress interface used for NAT (may be "" for auto-detect)

	// IsLAN marks an access-mode physical LAN/VLAN attachment. Attached VMs
	// have no Imp IPAM, gateway, DNS injection, NAT, or VTEP entry.
	IsLAN bool
	// VLANID of the attachment definition (0 = untagged onto parent bridge).
	VLANID int32
	// ParentInterface bound by the node profile for this attachment.
	ParentInterface string
	// DHCP is true when the guest obtains its address via DHCP on the LAN;
	// Firecracker then attaches the TAP without static IP configuration.
	DHCP bool

	DenyCIDRs       []string // host-enforced destination denies for this network (empty = none)
}

// NetManager abstracts host-level network operations for a VM.
// All methods must be idempotent.
type NetManager interface {
	// EnsureNetwork creates a bridge named bridgeName with gatewayIP/prefixLen
	// assigned to it, if it does not already exist.
	EnsureNetwork(ctx context.Context, bridgeName, gatewayIP string, prefixLen int) error

	// SetupVM creates a TAP device named tapName and attaches it to bridgeName.
	SetupVM(ctx context.Context, tapName, bridgeName, macAddr string) error

	// TeardownVM removes the TAP device named tapName. No-op if not found.
	TeardownVM(ctx context.Context, tapName string) error

	// EnsureNAT installs MASQUERADE rules for subnet via egressIface.
	// If egressIface is empty, the default-route interface is used.
	EnsureNAT(ctx context.Context, subnet, egressIface string) error

	// RemoveNAT removes the MASQUERADE rule for subnet.
	// Idempotent — no error if the rule does not exist.
	// If egressIface is empty, the default-route interface is used.
	RemoveNAT(ctx context.Context, subnet, egressIface string) error

	// EnsureEgressDeny installs host-level drops so that traffic sourced from
	// subnet towards any of denyCIDRs is discarded before MASQUERADE.
	// Implementations must reconcile: the installed rule set for this subnet
	// must exactly equal denyCIDRs after the call. Empty denyCIDRs removes
	// filtering for the subnet. Idempotent.
	EnsureEgressDeny(ctx context.Context, subnet string, denyCIDRs []string) error

	// RemoveEgressDeny removes all egress deny rules for subnet.
	// Idempotent — no error if none exist.
	RemoveEgressDeny(ctx context.Context, subnet string) error

	// EnsureVXLAN creates or reconciles the VXLAN interface for the given network,
	// attaches it to bridgeName, and brings it up. bridgeName must already exist
	// (call EnsureNetwork first). Idempotent.
	EnsureVXLAN(ctx context.Context, vni uint32, ifaceName, nodeIP, bridgeName string) error

	// SyncFDB reconciles the local FDB (forwarding database) on the VXLAN interface
	// to match the provided entries. Entries not in the list are removed.
	SyncFDB(ctx context.Context, ifaceName string, entries []FDBEntry) error
}

// StubNetManager is a no-op NetManager for tests.
// It records calls so tests can verify interactions.
type StubNetManager struct {
	EnsureNetworkCalls     []string // bridgeName
	SetupVMCalls           []string // tapName
	TeardownVMCalls        []string // tapName
	EnsureNATCalls         []string // subnet
	RemoveNATCalls         []string // subnet
	EnsureEgressDenyCalls  []EgressDenyCall
	RemoveEgressDenyCalls  []string // subnet
	EnsureVXLANCalls       []string // ifaceName
	EnsureVXLANBridgeCalls []string // bridgeName
	SyncFDBCalls           []string // ifaceName
	EnsureLANCalls         []string // "bridge|parent|vlanID"
	TeardownLANCalls       []string // "bridge|parent|vlanID"

	EnsureNetworkErr    error
	SetupVMErr          error
	TeardownVMErr       error
	EnsureNATErr        error
	RemoveNATErr        error
	EnsureEgressDenyErr error
	RemoveEgressDenyErr error
	EnsureVXLANErr      error
	SyncFDBErr          error
	EnsureLANErr        error
	TeardownLANErr      error
}

func (s *StubNetManager) EnsureNetwork(_ context.Context, bridgeName, _ string, _ int) error {
	s.EnsureNetworkCalls = append(s.EnsureNetworkCalls, bridgeName)
	return s.EnsureNetworkErr
}

func (s *StubNetManager) SetupVM(_ context.Context, tapName, _, _ string) error {
	s.SetupVMCalls = append(s.SetupVMCalls, tapName)
	return s.SetupVMErr
}

func (s *StubNetManager) TeardownVM(_ context.Context, tapName string) error {
	s.TeardownVMCalls = append(s.TeardownVMCalls, tapName)
	return s.TeardownVMErr
}

func (s *StubNetManager) EnsureNAT(_ context.Context, subnet, _ string) error {
	s.EnsureNATCalls = append(s.EnsureNATCalls, subnet)
	return s.EnsureNATErr
}

func (s *StubNetManager) RemoveNAT(_ context.Context, subnet, _ string) error {
	s.RemoveNATCalls = append(s.RemoveNATCalls, subnet)
	return s.RemoveNATErr
}

// EgressDenyCall records one EnsureEgressDeny invocation.
type EgressDenyCall struct {
	Subnet    string
	DenyCIDRs []string
}

func (s *StubNetManager) EnsureEgressDeny(_ context.Context, subnet string, denyCIDRs []string) error {
	copied := append([]string(nil), denyCIDRs...)
	sort.Strings(copied)
	s.EnsureEgressDenyCalls = append(s.EnsureEgressDenyCalls, EgressDenyCall{Subnet: subnet, DenyCIDRs: copied})
	return s.EnsureEgressDenyErr
}

func (s *StubNetManager) RemoveEgressDeny(_ context.Context, subnet string) error {
	s.RemoveEgressDenyCalls = append(s.RemoveEgressDenyCalls, subnet)
	return s.RemoveEgressDenyErr
}

func (s *StubNetManager) EnsureVXLAN(_ context.Context, _ uint32, ifaceName, _, bridgeName string) error {
	s.EnsureVXLANCalls = append(s.EnsureVXLANCalls, ifaceName)
	s.EnsureVXLANBridgeCalls = append(s.EnsureVXLANBridgeCalls, bridgeName)
	return s.EnsureVXLANErr
}

func (s *StubNetManager) SyncFDB(_ context.Context, ifaceName string, _ []FDBEntry) error {
	s.SyncFDBCalls = append(s.SyncFDBCalls, ifaceName)
	return s.SyncFDBErr
}

// EnsureLANBridge records the call so tests can verify interactions.
// The stub doubles as a LANAttacher for driver tests.
func (s *StubNetManager) EnsureLANBridge(_ context.Context, bridgeName, parentIface string, vlanID int32) error {
	s.EnsureLANCalls = append(s.EnsureLANCalls,
		bridgeName+"|"+parentIface+"|"+strconv.FormatInt(int64(vlanID), 10))
	return s.EnsureLANErr
}

// TeardownLANBridgeIfUnused records the call so tests can verify interactions.
func (s *StubNetManager) TeardownLANBridgeIfUnused(_ context.Context, bridgeName, parentIface string, vlanID int32) error {
	s.TeardownLANCalls = append(s.TeardownLANCalls,
		bridgeName+"|"+parentIface+"|"+strconv.FormatInt(int64(vlanID), 10))
	return s.TeardownLANErr
}

// compile-time assertions
var (
	_ NetManager  = (*StubNetManager)(nil)
	_ LANAttacher = (*StubNetManager)(nil)
)
