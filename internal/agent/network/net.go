package network

import "context"

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
	NATEnabled      bool     // true when NAT was enabled for this network
	EgressInterface string   // egress interface used for NAT (may be "" for auto-detect)
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

	// EnsureVXLAN creates or reconciles the VXLAN interface for the given network.
	// vni is the VXLAN Network Identifier. ifaceName is the interface name to use.
	// nodeIP is the local node's IP for VTEP termination.
	EnsureVXLAN(ctx context.Context, vni uint32, ifaceName, nodeIP string) error

	// SyncFDB reconciles the local FDB (forwarding database) on the VXLAN interface
	// to match the provided entries. Entries not in the list are removed.
	SyncFDB(ctx context.Context, ifaceName string, entries []FDBEntry) error
}

// StubNetManager is a no-op NetManager for tests.
// It records calls so tests can verify interactions.
type StubNetManager struct {
	EnsureNetworkCalls []string // bridgeName
	SetupVMCalls       []string // tapName
	TeardownVMCalls    []string // tapName
	EnsureNATCalls     []string // subnet
	RemoveNATCalls     []string // subnet
	EnsureVXLANCalls   []string // ifaceName
	SyncFDBCalls       []string // ifaceName

	EnsureNetworkErr error
	SetupVMErr       error
	TeardownVMErr    error
	EnsureNATErr     error
	RemoveNATErr     error
	EnsureVXLANErr   error
	SyncFDBErr       error
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

func (s *StubNetManager) EnsureVXLAN(_ context.Context, _ uint32, ifaceName, _ string) error {
	s.EnsureVXLANCalls = append(s.EnsureVXLANCalls, ifaceName)
	return s.EnsureVXLANErr
}

func (s *StubNetManager) SyncFDB(_ context.Context, ifaceName string, _ []FDBEntry) error {
	s.SyncFDBCalls = append(s.SyncFDBCalls, ifaceName)
	return s.SyncFDBErr
}

// compile-time assertion
var _ NetManager = (*StubNetManager)(nil)
