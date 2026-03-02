package network

import "context"

// NetworkInfo holds all networking state for a running VM.
type NetworkInfo struct {
	TAPName    string   // e.g. "imptap-a1b2c3d4"
	BridgeName string   // e.g. "impbr-e5f6a7b8"
	MACAddr    string   // e.g. "02:ab:cd:ef:01:23"
	IP         string   // VM's assigned IP, e.g. "192.168.100.2"
	PrefixLen  int      // subnet prefix length, e.g. 24
	Gateway    string   // bridge/gateway IP, e.g. "192.168.100.1"
	DNS        []string // nameservers injected into VM
	Subnet     string   // e.g. "192.168.100.0/24"
	NetworkKey string   // e.g. "default/mynet" — used by Allocator.Release
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
}

// StubNetManager is a no-op NetManager for tests.
// It records calls so tests can verify interactions.
type StubNetManager struct {
	EnsureNetworkCalls []string // bridgeName
	SetupVMCalls       []string // tapName
	TeardownVMCalls    []string // tapName
	EnsureNATCalls     []string // subnet

	EnsureNetworkErr error
	SetupVMErr       error
	TeardownVMErr    error
	EnsureNATErr     error
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

// compile-time assertion
var _ NetManager = (*StubNetManager)(nil)
