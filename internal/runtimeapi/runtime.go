// Package runtimeapi defines the local control protocol between an Imp agent
// and the node runtime that owns VM processes.
package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"sync"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/network"
)

// VMState is the runtime state returned by a node runtime.
type VMState struct {
	Running bool
	IP      string
	PID     int64
}

// SnapshotResult holds runtime-produced snapshot paths.
type SnapshotResult struct {
	StatePath string
	MemPath   string
}

// Backend is the deep runtime module interface. The transport deliberately
// exposes lifecycle intent, not Firecracker, network, or process internals.
type Backend interface {
	Start(context.Context, *impdevv1alpha1.ImpVM) (int64, error)
	Stop(context.Context, *impdevv1alpha1.ImpVM) error
	Get(context.Context, *impdevv1alpha1.ImpVM) (VMState, error)
	Snapshot(context.Context, *impdevv1alpha1.ImpVM, string) (SnapshotResult, error)
	Reattach(context.Context, *impdevv1alpha1.ImpVM) error
	IsAlive(int64) bool
	GetVSockPath(string) (string, bool)
	EnsureNetwork(context.Context, string, string, int) error
	SetupVM(context.Context, string, string, string) error
	TeardownVM(context.Context, string) error
	EnsureNAT(context.Context, string, string) error
	RemoveNAT(context.Context, string, string) error
	EnsureEgressDeny(context.Context, string, []string) error
	RemoveEgressDeny(context.Context, string) error
	EnsureVXLAN(context.Context, uint32, string, string, string) error
	SyncFDB(context.Context, string, []network.FDBEntry) error
}

// BackendFuncs makes protocol behavior testable without Firecracker.
type BackendFuncs struct {
	StartFunc            func(context.Context, *impdevv1alpha1.ImpVM) (int64, error)
	StopFunc             func(context.Context, *impdevv1alpha1.ImpVM) error
	GetFunc              func(context.Context, *impdevv1alpha1.ImpVM) (VMState, error)
	SnapshotFunc         func(context.Context, *impdevv1alpha1.ImpVM, string) (SnapshotResult, error)
	ReattachFunc         func(context.Context, *impdevv1alpha1.ImpVM) error
	IsAliveFunc          func(int64) bool
	GetVSockPathFunc     func(string) (string, bool)
	EnsureNetworkFunc    func(context.Context, string, string, int) error
	SetupVMFunc          func(context.Context, string, string, string) error
	TeardownVMFunc       func(context.Context, string) error
	EnsureNATFunc        func(context.Context, string, string) error
	RemoveNATFunc        func(context.Context, string, string) error
	EnsureEgressDenyFunc func(context.Context, string, []string) error
	RemoveEgressDenyFunc func(context.Context, string) error
	EnsureVXLANFunc      func(context.Context, uint32, string, string, string) error
	SyncFDBFunc          func(context.Context, string, []network.FDBEntry) error
}

func (f BackendFuncs) Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	if f.StartFunc == nil {
		return 0, errors.New("start is not supported")
	}
	return f.StartFunc(ctx, vm)
}

func (f BackendFuncs) Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	if f.StopFunc == nil {
		return errors.New("stop is not supported")
	}
	return f.StopFunc(ctx, vm)
}

func (f BackendFuncs) Get(ctx context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error) {
	if f.GetFunc == nil {
		return VMState{}, errors.New("get is not supported")
	}
	return f.GetFunc(ctx, vm)
}

func (f BackendFuncs) Snapshot(ctx context.Context, vm *impdevv1alpha1.ImpVM, dest string) (SnapshotResult, error) {
	if f.SnapshotFunc == nil {
		return SnapshotResult{}, errors.New("snapshot is not supported")
	}
	return f.SnapshotFunc(ctx, vm, dest)
}

func (f BackendFuncs) Reattach(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	if f.ReattachFunc == nil {
		return errors.New("reattach is not supported")
	}
	return f.ReattachFunc(ctx, vm)
}

func (f BackendFuncs) IsAlive(pid int64) bool {
	return f.IsAliveFunc != nil && f.IsAliveFunc(pid)
}

func (f BackendFuncs) GetVSockPath(key string) (string, bool) {
	if f.GetVSockPathFunc == nil {
		return "", false
	}
	return f.GetVSockPathFunc(key)
}

func (f BackendFuncs) EnsureNetwork(ctx context.Context, bridgeName, gatewayIP string, prefixLen int) error {
	if f.EnsureNetworkFunc == nil {
		return errors.New("ensure network is not supported")
	}
	return f.EnsureNetworkFunc(ctx, bridgeName, gatewayIP, prefixLen)
}
func (f BackendFuncs) SetupVM(ctx context.Context, tapName, bridgeName, macAddr string) error {
	if f.SetupVMFunc == nil {
		return errors.New("setup VM network is not supported")
	}
	return f.SetupVMFunc(ctx, tapName, bridgeName, macAddr)
}
func (f BackendFuncs) TeardownVM(ctx context.Context, tapName string) error {
	if f.TeardownVMFunc == nil {
		return errors.New("teardown VM network is not supported")
	}
	return f.TeardownVMFunc(ctx, tapName)
}
func (f BackendFuncs) EnsureNAT(ctx context.Context, subnet, egressInterface string) error {
	if f.EnsureNATFunc == nil {
		return errors.New("ensure NAT is not supported")
	}
	return f.EnsureNATFunc(ctx, subnet, egressInterface)
}
func (f BackendFuncs) RemoveNAT(ctx context.Context, subnet, egressInterface string) error {
	if f.RemoveNATFunc == nil {
		return errors.New("remove NAT is not supported")
	}
	return f.RemoveNATFunc(ctx, subnet, egressInterface)
}
func (f BackendFuncs) EnsureEgressDeny(ctx context.Context, subnet string, denyCIDRs []string) error {
	if f.EnsureEgressDenyFunc == nil {
		return errors.New("ensure egress deny is not supported")
	}
	return f.EnsureEgressDenyFunc(ctx, subnet, denyCIDRs)
}
func (f BackendFuncs) RemoveEgressDeny(ctx context.Context, subnet string) error {
	if f.RemoveEgressDenyFunc == nil {
		return errors.New("remove egress deny is not supported")
	}
	return f.RemoveEgressDenyFunc(ctx, subnet)
}
func (f BackendFuncs) EnsureVXLAN(ctx context.Context, vni uint32, ifaceName, nodeIP, bridgeName string) error {
	if f.EnsureVXLANFunc == nil {
		return errors.New("ensure VXLAN is not supported")
	}
	return f.EnsureVXLANFunc(ctx, vni, ifaceName, nodeIP, bridgeName)
}
func (f BackendFuncs) SyncFDB(ctx context.Context, ifaceName string, entries []network.FDBEntry) error {
	if f.SyncFDBFunc == nil {
		return errors.New("sync FDB is not supported")
	}
	return f.SyncFDBFunc(ctx, ifaceName, entries)
}

// Server serves one runtime backend over a node-local Unix socket.
type Server struct {
	backend  Backend
	rpc      *rpc.Server
	mu       sync.Mutex
	listener net.Listener
}

// NewServer constructs a server without starting its listener.
func NewServer(backend Backend) *Server {
	s := &Server{backend: backend, rpc: rpc.NewServer()}
	if err := s.rpc.RegisterName("Runtime", &rpcService{backend: backend}); err != nil {
		panic(fmt.Sprintf("register runtime RPC service: %v", err))
	}
	return s
}

// Start binds endpoint and begins serving. Endpoint permissions restrict access
// to Pods that receive the runtime socket hostPath mount.
func (s *Server) Start(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return errors.New("runtime server already started")
	}
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o750); err != nil {
		return fmt.Errorf("create runtime socket dir: %w", err)
	}
	if err := os.Remove(endpoint); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale runtime socket: %w", err)
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return fmt.Errorf("listen on runtime socket: %w", err)
	}
	if err := os.Chmod(endpoint, 0o660); err != nil { //nolint:gosec // agent and runtime require shared group socket access
		_ = listener.Close()
		return fmt.Errorf("set runtime socket permissions: %w", err)
	}
	s.listener = listener
	go s.rpc.Accept(listener)
	return nil
}

// Close stops the listener and removes the socket endpoint.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	endpoint := s.listener.Addr().String()
	err := s.listener.Close()
	s.listener = nil
	if removeErr := os.Remove(endpoint); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return err
}

type VMArgs struct{ VM *impdevv1alpha1.ImpVM }
type SnapshotArgs struct {
	VM      *impdevv1alpha1.ImpVM
	DestDir string
}
type PIDArgs struct{ PID int64 }
type KeyArgs struct{ Key string }
type Empty struct{}
type StartReply struct{ PID int64 }
type StateReply struct{ State VMState }
type SnapshotReply struct{ Result SnapshotResult }
type AliveReply struct{ Alive bool }
type VSockReply struct {
	Path  string
	Found bool
}
type EnsureNetworkArgs struct {
	BridgeName string
	GatewayIP  string
	PrefixLen  int
}
type SetupVMArgs struct {
	TAPName    string
	BridgeName string
	MACAddr    string
}
type TAPArgs struct{ TAPName string }
type NATArgs struct {
	Subnet          string
	EgressInterface string
}
type EgressDenyArgs struct {
	Subnet    string
	DenyCIDRs []string
}
type SubnetArgs struct{ Subnet string }
type VXLANArgs struct {
	VNI           uint32
	InterfaceName string
	NodeIP        string
	BridgeName    string
}
type FDBArgs struct {
	InterfaceName string
	Entries       []network.FDBEntry
}

type rpcService struct{ backend Backend }

func (s *rpcService) Start(args VMArgs, reply *StartReply) error {
	pid, err := s.backend.Start(context.Background(), args.VM)
	reply.PID = pid
	return err
}

func (s *rpcService) Stop(args VMArgs, _ *Empty) error {
	return s.backend.Stop(context.Background(), args.VM)
}

func (s *rpcService) Get(args VMArgs, reply *StateReply) error {
	state, err := s.backend.Get(context.Background(), args.VM)
	reply.State = state
	return err
}

func (s *rpcService) Snapshot(args SnapshotArgs, reply *SnapshotReply) error {
	result, err := s.backend.Snapshot(context.Background(), args.VM, args.DestDir)
	reply.Result = result
	return err
}

func (s *rpcService) Reattach(args VMArgs, _ *Empty) error {
	return s.backend.Reattach(context.Background(), args.VM)
}

func (s *rpcService) IsAlive(args PIDArgs, reply *AliveReply) error {
	reply.Alive = s.backend.IsAlive(args.PID)
	return nil
}

func (s *rpcService) GetVSockPath(args KeyArgs, reply *VSockReply) error {
	reply.Path, reply.Found = s.backend.GetVSockPath(args.Key)
	return nil
}

func (s *rpcService) EnsureNetwork(args EnsureNetworkArgs, _ *Empty) error {
	return s.backend.EnsureNetwork(context.Background(), args.BridgeName, args.GatewayIP, args.PrefixLen)
}
func (s *rpcService) SetupVM(args SetupVMArgs, _ *Empty) error {
	return s.backend.SetupVM(context.Background(), args.TAPName, args.BridgeName, args.MACAddr)
}
func (s *rpcService) TeardownVM(args TAPArgs, _ *Empty) error {
	return s.backend.TeardownVM(context.Background(), args.TAPName)
}
func (s *rpcService) EnsureNAT(args NATArgs, _ *Empty) error {
	return s.backend.EnsureNAT(context.Background(), args.Subnet, args.EgressInterface)
}
func (s *rpcService) RemoveNAT(args NATArgs, _ *Empty) error {
	return s.backend.RemoveNAT(context.Background(), args.Subnet, args.EgressInterface)
}
func (s *rpcService) EnsureEgressDeny(args EgressDenyArgs, _ *Empty) error {
	return s.backend.EnsureEgressDeny(context.Background(), args.Subnet, args.DenyCIDRs)
}
func (s *rpcService) RemoveEgressDeny(args SubnetArgs, _ *Empty) error {
	return s.backend.RemoveEgressDeny(context.Background(), args.Subnet)
}
func (s *rpcService) EnsureVXLAN(args VXLANArgs, _ *Empty) error {
	return s.backend.EnsureVXLAN(context.Background(), args.VNI, args.InterfaceName, args.NodeIP, args.BridgeName)
}
func (s *rpcService) SyncFDB(args FDBArgs, _ *Empty) error {
	return s.backend.SyncFDB(context.Background(), args.InterfaceName, args.Entries)
}

// Client invokes a node runtime through its Unix socket.
type Client struct{ endpoint string }

// NewClient returns a runtime client for endpoint.
func NewClient(endpoint string) *Client { return &Client{endpoint: endpoint} }

func (c *Client) call(ctx context.Context, method string, args, reply any) error {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.endpoint)
	if err != nil {
		return fmt.Errorf("dial runtime: %w", err)
	}
	defer conn.Close() //nolint:errcheck // connection is no longer useful after RPC call
	rpcClient := rpc.NewClient(conn)
	defer rpcClient.Close() //nolint:errcheck // connection close above owns cleanup
	return rpcClient.Call("Runtime."+method, args, reply)
}

// Start requests a VM be running and returns its runtime PID.
func (c *Client) Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	var reply StartReply
	if err := c.call(ctx, "Start", VMArgs{VM: vm}, &reply); err != nil {
		return 0, err
	}
	return reply.PID, nil
}

// Stop requests that a VM and its runtime resources be stopped.
func (c *Client) Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	return c.call(ctx, "Stop", VMArgs{VM: vm}, &Empty{})
}

// Get returns runtime-authoritative VM state.
func (c *Client) Get(ctx context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error) {
	var reply StateReply
	if err := c.call(ctx, "Get", VMArgs{VM: vm}, &reply); err != nil {
		return VMState{}, err
	}
	return reply.State, nil
}

// Snapshot requests a runtime snapshot to destDir.
func (c *Client) Snapshot(ctx context.Context, vm *impdevv1alpha1.ImpVM, destDir string) (SnapshotResult, error) {
	var reply SnapshotReply
	if err := c.call(ctx, "Snapshot", SnapshotArgs{VM: vm, DestDir: destDir}, &reply); err != nil {
		return SnapshotResult{}, err
	}
	return reply.Result, nil
}

// Reattach asks the runtime to restore VM state after an agent restart.
func (c *Client) Reattach(ctx context.Context, vm *impdevv1alpha1.ImpVM) error {
	return c.call(ctx, "Reattach", VMArgs{VM: vm}, &Empty{})
}

// IsAlive checks runtime process liveness by PID.
func (c *Client) IsAlive(ctx context.Context, pid int64) (bool, error) {
	var reply AliveReply
	if err := c.call(ctx, "IsAlive", PIDArgs{PID: pid}, &reply); err != nil {
		return false, err
	}
	return reply.Alive, nil
}

// GetVSockPath returns a running VM's VSOCK proxy path.
func (c *Client) GetVSockPath(ctx context.Context, key string) (string, bool, error) {
	var reply VSockReply
	if err := c.call(ctx, "GetVSockPath", KeyArgs{Key: key}, &reply); err != nil {
		return "", false, err
	}
	return reply.Path, reply.Found, nil
}

func (c *Client) EnsureNetwork(ctx context.Context, bridgeName, gatewayIP string, prefixLen int) error {
	return c.call(ctx, "EnsureNetwork", EnsureNetworkArgs{BridgeName: bridgeName, GatewayIP: gatewayIP, PrefixLen: prefixLen}, &Empty{})
}
func (c *Client) SetupVM(ctx context.Context, tapName, bridgeName, macAddr string) error {
	return c.call(ctx, "SetupVM", SetupVMArgs{TAPName: tapName, BridgeName: bridgeName, MACAddr: macAddr}, &Empty{})
}
func (c *Client) TeardownVM(ctx context.Context, tapName string) error {
	return c.call(ctx, "TeardownVM", TAPArgs{TAPName: tapName}, &Empty{})
}
func (c *Client) EnsureNAT(ctx context.Context, subnet, egressInterface string) error {
	return c.call(ctx, "EnsureNAT", NATArgs{Subnet: subnet, EgressInterface: egressInterface}, &Empty{})
}
func (c *Client) RemoveNAT(ctx context.Context, subnet, egressInterface string) error {
	return c.call(ctx, "RemoveNAT", NATArgs{Subnet: subnet, EgressInterface: egressInterface}, &Empty{})
}
func (c *Client) EnsureEgressDeny(ctx context.Context, subnet string, denyCIDRs []string) error {
	return c.call(ctx, "EnsureEgressDeny", EgressDenyArgs{Subnet: subnet, DenyCIDRs: denyCIDRs}, &Empty{})
}
func (c *Client) RemoveEgressDeny(ctx context.Context, subnet string) error {
	return c.call(ctx, "RemoveEgressDeny", SubnetArgs{Subnet: subnet}, &Empty{})
}
func (c *Client) EnsureVXLAN(ctx context.Context, vni uint32, ifaceName, nodeIP, bridgeName string) error {
	return c.call(ctx, "EnsureVXLAN", VXLANArgs{VNI: vni, InterfaceName: ifaceName, NodeIP: nodeIP, BridgeName: bridgeName}, &Empty{})
}
func (c *Client) SyncFDB(ctx context.Context, ifaceName string, entries []network.FDBEntry) error {
	return c.call(ctx, "SyncFDB", FDBArgs{InterfaceName: ifaceName, Entries: entries}, &Empty{})
}
