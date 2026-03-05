# Agent-Side Networking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add TAP/bridge networking and NAT to the Imp agent so Firecracker VMs join an ImpNetwork with a real IP address and outbound connectivity.

**Architecture:** A new `internal/agent/network` package provides:
- `NetManager` interface — bridge+TAP via `vishvananda/netlink`; NAT via `nft`/`iptables` exec
- `Allocator` — pure-Go in-memory IP allocator, CIDR-aware
- `StubNetManager` — test double for use in driver unit tests

`FirecrackerDriver` gains `Net NetManager` and `Alloc *network.Allocator` fields. In `Start`, if `vm.Spec.NetworkRef` is set, the driver fetches the ImpNetwork, derives bridge/TAP names, allocates an IP, calls `Net.EnsureNetwork`+`Net.SetupVM`, then injects the TAP into `buildConfig` via `StaticNetworkConfiguration`+`IPConfiguration` (the SDK auto-injects the `ip=` kernel boot param). In `Stop`, the TAP is torn down and the IP released. `Inspect` returns the VM's IP from the stored `fcProc.netInfo`.

**Tech Stack:** Go, `vishvananda/netlink v1.1.1` (already in go.mod as indirect), `firecracker-go-sdk v1.0.0`, `exec` for nft/iptables, standard `testing` package for unit tests, `//go:build linux` for all kernel-facing code.

---

### Task 1: network package — interface, types, names, StubNetManager

**Files:**
- Create: `internal/agent/network/net.go`
- Create: `internal/agent/network/names.go`
- Create: `internal/agent/network/names_test.go`

**Step 1: Write the failing tests** (`internal/agent/network/names_test.go`)

```go
package network_test

import (
	"regexp"
	"testing"

	"github.com/syscode-labs/imp/internal/agent/network"
)

func TestBridgeName_length(t *testing.T) {
	for _, key := range []string{"default/my-net", "prod/network-with-long-name", ""} {
		got := network.BridgeName(key)
		if len(got) > 15 {
			t.Errorf("BridgeName(%q) = %q (len %d), exceeds 15", key, got, len(got))
		}
		if len(got) == 0 {
			t.Errorf("BridgeName(%q) returned empty string", key)
		}
	}
}

func TestBridgeName_deterministic(t *testing.T) {
	if network.BridgeName("default/net") != network.BridgeName("default/net") {
		t.Error("BridgeName is not deterministic")
	}
}

func TestBridgeName_distinct(t *testing.T) {
	if network.BridgeName("a/net1") == network.BridgeName("a/net2") {
		t.Error("different keys should produce different names (hash collision)")
	}
}

func TestTAPName_length(t *testing.T) {
	for _, key := range []string{"default/my-vm", "prod/vm-with-long-name", ""} {
		got := network.TAPName(key)
		if len(got) > 15 {
			t.Errorf("TAPName(%q) = %q (len %d), exceeds 15", key, got, len(got))
		}
	}
}

func TestMACAddr_format(t *testing.T) {
	re := regexp.MustCompile(`^02:[0-9a-f]{2}(:[0-9a-f]{2}){4}$`)
	for _, key := range []string{"default/vm1", "ns/vm2"} {
		got := network.MACAddr(key)
		if !re.MatchString(got) {
			t.Errorf("MACAddr(%q) = %q, want 02:xx:xx:xx:xx:xx", key, got)
		}
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/giovanni/syscode/git/imp
go test ./internal/agent/network/...
```
Expected: `cannot find package` (package doesn't exist yet)

**Step 3: Create `internal/agent/network/net.go`**

```go
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
```

**Step 4: Create `internal/agent/network/names.go`**

```go
package network

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// BridgeName returns a deterministic Linux bridge name for a network key
// (e.g. "default/mynet"). Length is always 14 characters.
func BridgeName(netKey string) string {
	h := sha256.Sum256([]byte(netKey))
	return fmt.Sprintf("impbr-%08x", binary.BigEndian.Uint32(h[:4]))
}

// TAPName returns a deterministic TAP device name for a VM key
// (e.g. "default/my-vm"). Length is always 15 characters.
func TAPName(vmKey string) string {
	h := sha256.Sum256([]byte(vmKey))
	return fmt.Sprintf("imptap-%08x", binary.BigEndian.Uint32(h[:4]))
}

// MACAddr returns a deterministic locally-administered unicast MAC address
// for a VM key. Format: "02:xx:xx:xx:xx:xx".
func MACAddr(vmKey string) string {
	h := sha256.Sum256([]byte(vmKey))
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", h[0], h[1], h[2], h[3], h[4])
}
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/agent/network/...
```
Expected: PASS (4 test functions)

**Step 6: Commit**

```bash
git add internal/agent/network/net.go internal/agent/network/names.go internal/agent/network/names_test.go
git commit -m "feat(agent/network): NetManager interface, NetworkInfo, StubNetManager, name helpers"
```

---

### Task 2: IP Allocator

**Files:**
- Create: `internal/agent/network/ipallocator.go`
- Create: `internal/agent/network/ipallocator_test.go`

**Step 1: Write the failing tests** (`internal/agent/network/ipallocator_test.go`)

```go
package network_test

import (
	"testing"

	"github.com/syscode-labs/imp/internal/agent/network"
)

func TestAllocator_basic(t *testing.T) {
	a := network.NewAllocator()
	ip, err := a.Allocate("default/net", "192.168.100.0/24", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default gateway is .1; first allocatable IP is .2.
	if ip != "192.168.100.2" {
		t.Errorf("got %q, want %q", ip, "192.168.100.2")
	}
}

func TestAllocator_skipsGateway(t *testing.T) {
	a := network.NewAllocator()
	// Allocate with explicit gateway = .5
	ip, err := a.Allocate("ns/net", "10.0.0.0/24", "10.0.0.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// .1 is first free (gateway .5 is only reserved once it's reached)
	if ip == "10.0.0.5" {
		t.Error("allocated the gateway address")
	}
}

func TestAllocator_sequentialAllocations(t *testing.T) {
	a := network.NewAllocator()
	ip1, _ := a.Allocate("ns/net", "10.1.0.0/24", "10.1.0.1")
	ip2, _ := a.Allocate("ns/net", "10.1.0.0/24", "10.1.0.1")
	if ip1 == ip2 {
		t.Errorf("expected distinct IPs, both got %q", ip1)
	}
}

func TestAllocator_release(t *testing.T) {
	a := network.NewAllocator()
	ip, _ := a.Allocate("ns/net", "10.2.0.0/24", "10.2.0.1")
	a.Release("ns/net", ip)
	ip2, err := a.Allocate("ns/net", "10.2.0.0/24", "10.2.0.1")
	if err != nil {
		t.Fatalf("unexpected error after release: %v", err)
	}
	if ip2 != ip {
		t.Errorf("expected reuse of released IP %q, got %q", ip, ip2)
	}
}

func TestAllocator_reserve(t *testing.T) {
	a := network.NewAllocator()
	a.Reserve("ns/net", "10.3.0.2")
	ip, _ := a.Allocate("ns/net", "10.3.0.0/24", "10.3.0.1")
	if ip == "10.3.0.2" {
		t.Error("reserved IP was re-allocated")
	}
}

func TestAllocator_exhaustion(t *testing.T) {
	a := network.NewAllocator()
	// /30 has 2 usable hosts (.1 = gateway, .2 = only free IP)
	_, _ = a.Allocate("ns/net", "10.4.0.0/30", "10.4.0.1")
	_, err := a.Allocate("ns/net", "10.4.0.0/30", "10.4.0.1")
	if err == nil {
		t.Error("expected error when subnet exhausted")
	}
}

func TestAllocator_multipleNetworks(t *testing.T) {
	a := network.NewAllocator()
	ip1, _ := a.Allocate("ns/net1", "10.5.0.0/24", "")
	ip2, _ := a.Allocate("ns/net2", "10.5.0.0/24", "")
	if ip1 != ip2 {
		t.Error("different network keys should have independent allocation state")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/agent/network/...
```
Expected: FAIL `undefined: network.NewAllocator`

**Step 3: Create `internal/agent/network/ipallocator.go`**

```go
package network

import (
	"fmt"
	"net"
	"sync"
)

// Allocator manages in-memory IP allocation per network.
// Each network is keyed by its namespace/name string.
// Safe for concurrent use.
type Allocator struct {
	mu        sync.Mutex
	allocated map[string]map[string]struct{} // netKey → set of IPs
}

// NewAllocator returns an empty Allocator.
func NewAllocator() *Allocator {
	return &Allocator{allocated: make(map[string]map[string]struct{})}
}

// Allocate returns the next free IP in subnet for the given network key.
// gateway is reserved and never returned; if empty, the first host address
// (network+1) is used as the gateway. Returns an error if the subnet is full.
func (a *Allocator) Allocate(netKey, subnet, gateway string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	_, cidr, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("parse subnet %q: %w", subnet, err)
	}

	var gwIP net.IP
	if gateway == "" {
		gwIP = nextIP(cidr.IP.To4())
	} else {
		gwIP = net.ParseIP(gateway).To4()
	}

	set := a.allocated[netKey]
	if set == nil {
		set = make(map[string]struct{})
		a.allocated[netKey] = set
	}

	bcast := broadcastIP(cidr)
	ip := nextIP(cidr.IP.To4()) // skip network address
	for cidr.Contains(ip) {
		if ip.Equal(bcast) {
			break
		}
		s := ip.String()
		if !ip.Equal(gwIP) {
			if _, used := set[s]; !used {
				set[s] = struct{}{}
				return s, nil
			}
		}
		ip = nextIP(ip)
	}
	return "", fmt.Errorf("no free IPs in subnet %s", subnet)
}

// Release frees a previously allocated IP so it can be reused.
func (a *Allocator) Release(netKey, ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if set, ok := a.allocated[netKey]; ok {
		delete(set, ip)
	}
}

// Reserve marks an IP as in-use without going through Allocate.
// Use this during startup to re-register IPs from existing running VMs.
func (a *Allocator) Reserve(netKey, ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.allocated[netKey] == nil {
		a.allocated[netKey] = make(map[string]struct{})
	}
	a.allocated[netKey][ip] = struct{}{}
}

// nextIP returns a copy of ip with the last octet incremented (wraps carry).
func nextIP(ip net.IP) net.IP {
	ip = ip.To4()
	next := make(net.IP, 4)
	copy(next, ip)
	for i := 3; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			break
		}
	}
	return next
}

// broadcastIP returns the broadcast address of cidr.
func broadcastIP(cidr *net.IPNet) net.IP {
	ip := cidr.IP.To4()
	mask := cidr.Mask
	bcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		bcast[i] = ip[i] | ^mask[i]
	}
	return bcast
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/agent/network/...
```
Expected: PASS (all allocator + names tests)

**Step 5: Commit**

```bash
git add internal/agent/network/ipallocator.go internal/agent/network/ipallocator_test.go
git commit -m "feat(agent/network): in-memory IP allocator"
```

---

### Task 3: LinuxNetManager (bridge + TAP + NAT)

**Files:**
- Create: `internal/agent/network/linux.go` (`//go:build linux`)

This code requires root and a real Linux kernel, so the integration tests are skipped in CI.
The goal here is: correct, compiling code that passes the unit test (compile + interface check).

**Step 1: Write the failing test** (add to `internal/agent/network/names_test.go` or a new file)

Create `internal/agent/network/linux_test.go`:

```go
//go:build linux

package network_test

import (
	"testing"

	"github.com/syscode-labs/imp/internal/agent/network"
)

func TestLinuxNetManager_implementsInterface(t *testing.T) {
	// compile-time interface check — if this compiles, the interface is satisfied
	var _ network.NetManager = (*network.LinuxNetManager)(nil)
}

func TestLinuxNetManager_newDoesNotPanic(t *testing.T) {
	mgr := network.NewLinuxNetManager()
	if mgr == nil {
		t.Fatal("NewLinuxNetManager returned nil")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test -tags linux ./internal/agent/network/...
```
Expected: FAIL — `undefined: network.LinuxNetManager`

**Step 3: Create `internal/agent/network/linux.go`**

```go
//go:build linux

package network

import (
	"context"
	"fmt"
	"net"
	"os/exec"

	"github.com/vishvananda/netlink"
)

// LinuxNetManager implements NetManager using netlink (bridge+TAP) and
// nft or iptables (NAT). Safe for concurrent use; netlink calls are
// serialised by the kernel.
type LinuxNetManager struct {
	natBackend string // "nftables" or "iptables"
}

// NewLinuxNetManager creates a LinuxNetManager and auto-detects the NAT backend.
// Prefers nftables; falls back to iptables if nft binary is not in PATH.
func NewLinuxNetManager() *LinuxNetManager {
	backend := "nftables"
	if _, err := exec.LookPath("nft"); err != nil {
		backend = "iptables"
	}
	return &LinuxNetManager{natBackend: backend}
}

// EnsureNetwork creates bridge bridgeName with gatewayIP/prefixLen if it does
// not already exist, then sets it up. Idempotent.
func (m *LinuxNetManager) EnsureNetwork(_ context.Context, bridgeName, gatewayIP string, prefixLen int) error {
	link, err := netlink.LinkByName(bridgeName)
	if err == nil {
		// Bridge already exists — ensure it is up.
		return netlink.LinkSetUp(link)
	}

	br := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}
	if err := netlink.LinkAdd(br); err != nil {
		return fmt.Errorf("create bridge %s: %w", bridgeName, err)
	}

	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP(gatewayIP).To4(),
			Mask: net.CIDRMask(prefixLen, 32),
		},
	}
	// Re-fetch link after creation to get updated attrs.
	link, err = netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("fetch bridge %s after create: %w", bridgeName, err)
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("assign %s/%d to bridge %s: %w", gatewayIP, prefixLen, bridgeName, err)
	}
	return netlink.LinkSetUp(link)
}

// SetupVM creates TAP device tapName, attaches it to bridgeName, and sets it up.
func (m *LinuxNetManager) SetupVM(_ context.Context, tapName, bridgeName, _ string) error {
	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{Name: tapName},
		Mode:      netlink.TUNTAP_MODE_TAP,
	}
	if err := netlink.LinkAdd(tap); err != nil {
		return fmt.Errorf("create tap %s: %w", tapName, err)
	}

	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("get bridge %s: %w", bridgeName, err)
	}

	tapLink, err := netlink.LinkByName(tapName)
	if err != nil {
		return fmt.Errorf("get tap %s after create: %w", tapName, err)
	}
	if err := netlink.LinkSetMaster(tapLink, br); err != nil {
		return fmt.Errorf("attach %s to bridge %s: %w", tapName, bridgeName, err)
	}
	return netlink.LinkSetUp(tapLink)
}

// TeardownVM removes TAP device tapName. No-op if the device does not exist.
func (m *LinuxNetManager) TeardownVM(_ context.Context, tapName string) error {
	link, err := netlink.LinkByName(tapName)
	if err != nil {
		// LinkNotFoundError: already gone — treat as success.
		return nil
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete tap %s: %w", tapName, err)
	}
	return nil
}

// EnsureNAT installs a MASQUERADE rule for subnet on egressIface.
// If egressIface is empty, the default-route interface is auto-detected.
// Uses nftables if available, otherwise iptables. Idempotent (errors on
// duplicate rules are suppressed).
func (m *LinuxNetManager) EnsureNAT(_ context.Context, subnet, egressIface string) error {
	if egressIface == "" {
		iface, err := defaultRouteIface()
		if err != nil {
			return fmt.Errorf("detect egress interface: %w", err)
		}
		egressIface = iface
	}
	if m.natBackend == "nftables" {
		return ensureNATNftables(subnet, egressIface)
	}
	return ensureNATIptables(subnet, egressIface)
}

// defaultRouteIface returns the network interface associated with the default route.
func defaultRouteIface() (string, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return "", fmt.Errorf("list routes: %w", err)
	}
	for _, r := range routes {
		if r.Dst == nil { // default route: Dst == nil
			link, err := netlink.LinkByIndex(r.LinkIndex)
			if err != nil {
				return "", fmt.Errorf("get default route link: %w", err)
			}
			return link.Attrs().Name, nil
		}
	}
	return "", fmt.Errorf("no default route found")
}

// ensureNATNftables adds a MASQUERADE rule via nft.
// Creates the imp_nat table+chain if absent.
func ensureNATNftables(subnet, egressIface string) error {
	// Ensure table exists (fails silently if already present).
	_ = exec.Command("nft", "add", "table", "ip", "imp_nat").Run() //nolint:errcheck,gosec
	// Ensure postrouting chain exists.
	_ = exec.Command("nft", "add", "chain", "ip", "imp_nat", "postrouting", //nolint:errcheck,gosec
		"{ type nat hook postrouting priority 100; }").Run()
	// Add masquerade rule. Duplicate rules are harmless with nftables.
	rule := fmt.Sprintf("ip saddr %s oifname %q masquerade", subnet, egressIface)
	//nolint:gosec // G204: subnet and egressIface are validated upstream
	if out, err := exec.Command("nft", "add", "rule", "ip", "imp_nat", "postrouting", rule).CombinedOutput(); err != nil {
		return fmt.Errorf("nft add rule: %w: %s", err, out)
	}
	return nil
}

// ensureNATIptables adds a MASQUERADE rule via iptables. Skips if the rule exists.
func ensureNATIptables(subnet, egressIface string) error {
	// -C checks for existence; if it succeeds, rule already present.
	check := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", //nolint:gosec
		"-s", subnet, "-o", egressIface, "-j", "MASQUERADE")
	if check.Run() == nil {
		return nil // already installed
	}
	//nolint:gosec // G204: inputs validated upstream
	if out, err := exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
		"-s", subnet, "-o", egressIface, "-j", "MASQUERADE").CombinedOutput(); err != nil {
		return fmt.Errorf("iptables: %w: %s", err, out)
	}
	return nil
}

// compile-time assertion
var _ NetManager = (*LinuxNetManager)(nil)
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/agent/network/...
```
Expected: PASS (all tests, including linux_test.go on macOS the linux-tagged file is skipped but compilation is still checked)

Actually on macOS the `//go:build linux` file won't compile. Run the standard tests only:
```bash
go test $(go list ./internal/agent/network/... | grep -v 'linux')
```
Or simply:
```bash
go test ./internal/agent/network/...
```
The names+allocator tests should pass. The linux_test.go is only built on linux.

**Step 5: Verify the package compiles on Linux (cross-compile)**

```bash
GOOS=linux go build ./internal/agent/network/...
```
Expected: no errors

**Step 6: Commit**

```bash
git add internal/agent/network/linux.go internal/agent/network/linux_test.go
git commit -m "feat(agent/network): LinuxNetManager — bridge, TAP, NAT (nftables/iptables)"
```

---

### Task 4: Wire NetManager + Allocator into FirecrackerDriver

**Files:**
- Modify: `internal/agent/firecracker_driver.go`
- Modify: `internal/agent/firecracker_driver_test.go`

**Step 1: Write the failing tests** (add to `firecracker_driver_test.go`)

Add these test functions at the end of `internal/agent/firecracker_driver_test.go`:

```go
func TestFirecrackerDriver_BuildConfig_WithNetInfo(t *testing.T) {
	d := &FirecrackerDriver{
		KernelPath: "/boot/vmlinux",
		KernelArgs: "console=ttyS0 reboot=k panic=1 pci=off",
	}

	class := &impdevv1alpha1.ImpVMClass{}
	class.Spec.VCPU = 1
	class.Spec.MemoryMiB = 256

	ni := &network.NetworkInfo{
		TAPName:   "imptap-aabbccdd",
		MACAddr:   "02:aa:bb:cc:dd:ee",
		IP:        "192.168.100.2",
		PrefixLen: 24,
		Gateway:   "192.168.100.1",
		DNS:       []string{"8.8.8.8"},
	}

	cfg := d.buildConfig(class, "/cache/root.ext4", "/run/imp/s/vm.sock", ni)

	if len(cfg.NetworkInterfaces) != 1 {
		t.Fatalf("len(NetworkInterfaces) = %d, want 1", len(cfg.NetworkInterfaces))
	}
	iface := cfg.NetworkInterfaces[0]
	if iface.StaticConfiguration == nil {
		t.Fatal("StaticConfiguration is nil")
	}
	if iface.StaticConfiguration.HostDevName != "imptap-aabbccdd" {
		t.Errorf("HostDevName = %q, want %q", iface.StaticConfiguration.HostDevName, "imptap-aabbccdd")
	}
	if iface.StaticConfiguration.MacAddress != "02:aa:bb:cc:dd:ee" {
		t.Errorf("MacAddress = %q, want %q", iface.StaticConfiguration.MacAddress, "02:aa:bb:cc:dd:ee")
	}
	if iface.StaticConfiguration.IPConfiguration == nil {
		t.Fatal("IPConfiguration is nil")
	}
	if iface.StaticConfiguration.IPConfiguration.IPAddr.IP.String() != "192.168.100.2" {
		t.Errorf("IP = %q, want 192.168.100.2", iface.StaticConfiguration.IPConfiguration.IPAddr.IP)
	}
}

func TestFirecrackerDriver_BuildConfig_WithoutNetInfo(t *testing.T) {
	d := &FirecrackerDriver{KernelPath: "/boot/vmlinux", KernelArgs: "console=ttyS0"}
	class := &impdevv1alpha1.ImpVMClass{}
	class.Spec.VCPU = 1
	class.Spec.MemoryMiB = 256

	cfg := d.buildConfig(class, "/cache/root.ext4", "/run/imp/s/vm.sock", nil)

	if len(cfg.NetworkInterfaces) != 0 {
		t.Errorf("expected no NetworkInterfaces when netInfo is nil, got %d", len(cfg.NetworkInterfaces))
	}
}

func TestFirecrackerDriver_Inspect_ReturnsNetworkIP(t *testing.T) {
	d := &FirecrackerDriver{procs: make(map[string]*fcProc)}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "net-vm"

	d.procs[vmKey(vm)] = &fcProc{
		pid: int64(os.Getpid()),
		netInfo: &network.NetworkInfo{IP: "192.168.1.5"},
	}

	state, err := d.Inspect(context.Background(), vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !state.Running {
		t.Error("expected Running=true")
	}
	if state.IP != "192.168.1.5" {
		t.Errorf("IP = %q, want %q", state.IP, "192.168.1.5")
	}
}

func TestFirecrackerDriver_Stop_TeardownVMCalled(t *testing.T) {
	stub := &network.StubNetManager{}
	d := &FirecrackerDriver{
		Net:   stub,
		Alloc: network.NewAllocator(),
		procs: make(map[string]*fcProc),
	}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "net-vm-stop"

	d.procs[vmKey(vm)] = &fcProc{
		pid: 99999, // not running, but we're only testing teardown
		netInfo: &network.NetworkInfo{
			TAPName:    "imptap-deadbeef",
			NetworkKey: "default/mynet",
			IP:         "10.0.0.2",
		},
	}

	if err := d.Stop(context.Background(), vm); err != nil {
		t.Fatalf("unexpected Stop error: %v", err)
	}
	if len(stub.TeardownVMCalls) != 1 || stub.TeardownVMCalls[0] != "imptap-deadbeef" {
		t.Errorf("TeardownVMCalls = %v, want [imptap-deadbeef]", stub.TeardownVMCalls)
	}
}
```

Also update the import block in `firecracker_driver_test.go` to add `network` package:
```go
import (
    // ... existing imports ...
    "github.com/syscode-labs/imp/internal/agent/network"
)
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/agent/...
```
Expected: compile errors — `network.NetworkInfo` undefined, `buildConfig` wrong signature, etc.

**Step 3: Modify `internal/agent/firecracker_driver.go`**

Changes needed:

1. **Add import** for `net` and `network` packages.

2. **Add `Net` and `Alloc` fields to `FirecrackerDriver`**:
```go
type FirecrackerDriver struct {
    // ... existing fields ...
    // Net manages host-level networking (bridge, TAP, NAT). May be nil if
    // no VMs on this node use a NetworkRef.
    Net network.NetManager
    // Alloc manages in-memory IP allocation per ImpNetwork.
    Alloc *network.Allocator
    // ...
}
```

3. **Add `netInfo` to `fcProc`**:
```go
type fcProc struct {
    machine  *firecracker.Machine
    pid      int64
    socket   string
    netInfo  *network.NetworkInfo // nil when NetworkRef is absent
}
```

4. **Update `Start`** — add network setup after rootfs build, before `buildConfig`:
```go
// Phase 2: set up networking if NetworkRef is set.
var netInfo *network.NetworkInfo
if vm.Spec.NetworkRef != nil && d.Net != nil {
    ni, err := d.setupNetwork(ctx, vm)
    if err != nil {
        return 0, fmt.Errorf("setup network: %w", err)
    }
    netInfo = ni
}
```

5. **Add `setupNetwork` method**:
```go
func (d *FirecrackerDriver) setupNetwork(ctx context.Context, vm *impdevv1alpha1.ImpVM) (*network.NetworkInfo, error) {
    var net impdevv1alpha1.ImpNetwork
    if err := d.Client.Get(ctx, ctrlclient.ObjectKey{
        Namespace: vm.Namespace,
        Name:      vm.Spec.NetworkRef.Name,
    }, &net); err != nil {
        return nil, fmt.Errorf("get network %q: %w", vm.Spec.NetworkRef.Name, err)
    }

    netKey := net.Namespace + "/" + net.Name
    vmKey := vmKey(vm)
    bridgeName := network.BridgeName(netKey)
    tapName := network.TAPName(vmKey)
    macAddr := network.MACAddr(vmKey)

    gateway := net.Spec.Gateway
    _, cidr, err := gonet.ParseCIDR(net.Spec.Subnet)
    if err != nil {
        return nil, fmt.Errorf("parse subnet %q: %w", net.Spec.Subnet, err)
    }
    prefixLen, _ := cidr.Mask.Size()
    if gateway == "" {
        gw := make(gonet.IP, 4)
        copy(gw, cidr.IP.To4())
        gw[3]++
        gateway = gw.String()
    }

    // Allocate VM IP.
    ip, err := d.Alloc.Allocate(netKey, net.Spec.Subnet, gateway)
    if err != nil {
        return nil, fmt.Errorf("allocate IP: %w", err)
    }

    // Ensure bridge exists with gateway IP.
    if err := d.Net.EnsureNetwork(ctx, bridgeName, gateway, prefixLen); err != nil {
        d.Alloc.Release(netKey, ip)
        return nil, fmt.Errorf("ensure bridge: %w", err)
    }

    // Create TAP and attach to bridge.
    if err := d.Net.SetupVM(ctx, tapName, bridgeName, macAddr); err != nil {
        d.Alloc.Release(netKey, ip)
        return nil, fmt.Errorf("setup tap: %w", err)
    }

    // Install NAT if requested (best-effort — don't block VM start).
    if net.Spec.NAT.Enabled {
        if natErr := d.Net.EnsureNAT(ctx, net.Spec.Subnet, net.Spec.NAT.EgressInterface); natErr != nil {
            logf.FromContext(ctx).Error(natErr, "EnsureNAT failed — VM will start without NAT")
        }
    }

    dns := net.Spec.DNS
    return &network.NetworkInfo{
        TAPName:    tapName,
        BridgeName: bridgeName,
        MACAddr:    macAddr,
        IP:         ip,
        PrefixLen:  prefixLen,
        Gateway:    gateway,
        DNS:        dns,
        Subnet:     net.Spec.Subnet,
        NetworkKey: netKey,
    }, nil
}
```

Note: rename `net` import alias to avoid collision — use `gonet "net"` for the stdlib net package.

6. **Update `buildConfig` signature and body** to accept `*network.NetworkInfo`:
```go
func (d *FirecrackerDriver) buildConfig(
    class *impdevv1alpha1.ImpVMClass,
    rootfsPath, socketPath string,
    netInfo *network.NetworkInfo,
) firecracker.Config {
    cfg := firecracker.Config{
        SocketPath:      socketPath,
        KernelImagePath: d.KernelPath,
        KernelArgs:      d.KernelArgs,
        Drives: []models.Drive{ /* ... unchanged ... */ },
        MachineCfg: models.MachineConfiguration{ /* ... unchanged ... */ },
    }
    if netInfo != nil {
        cfg.NetworkInterfaces = firecracker.NetworkInterfaces{{
            StaticConfiguration: &firecracker.StaticNetworkConfiguration{
                MacAddress:  netInfo.MACAddr,
                HostDevName: netInfo.TAPName,
                IPConfiguration: &firecracker.IPConfiguration{
                    IPAddr: gonet.IPNet{
                        IP:   gonet.ParseIP(netInfo.IP).To4(),
                        Mask: gonet.CIDRMask(netInfo.PrefixLen, 32),
                    },
                    Gateway:     gonet.ParseIP(netInfo.Gateway),
                    Nameservers: netInfo.DNS,
                },
            },
        }}
    }
    return cfg
}
```

7. **Update the `buildConfig` call in `Start`**:
```go
cfg := d.buildConfig(&class, rootfsPath, sockPath, netInfo)
```

8. **Update `Stop`** to tear down network:
```go
// After removing the socket, before returning:
if proc.netInfo != nil && d.Net != nil {
    if err := d.Net.TeardownVM(ctx, proc.netInfo.TAPName); err != nil {
        logf.FromContext(ctx).Error(err, "TeardownVM failed", "tap", proc.netInfo.TAPName)
    }
    if d.Alloc != nil {
        d.Alloc.Release(proc.netInfo.NetworkKey, proc.netInfo.IP)
    }
}
```

9. **Update `Inspect`** to return IP from netInfo:
```go
// Change the return at the end of Inspect from:
return VMState{Running: true, PID: proc.pid}, nil
// to:
ip := ""
if proc.netInfo != nil {
    ip = proc.netInfo.IP
}
return VMState{Running: true, PID: proc.pid, IP: ip}, nil
```

10. **Update `NewFirecrackerDriver`** to initialise `Alloc`:
```go
return &FirecrackerDriver{
    // ... existing fields ...
    Alloc: network.NewAllocator(),
    procs: make(map[string]*fcProc),
}, nil
```
(`Net` is set separately by `cmd/agent/driver_linux.go` after construction.)

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/agent/...
```
Expected: PASS (all existing + new tests)

**Step 5: Commit**

```bash
git add internal/agent/firecracker_driver.go internal/agent/firecracker_driver_test.go
git commit -m "feat(agent): wire NetManager + IP allocator into FirecrackerDriver"
```

---

### Task 5: Wire LinuxNetManager into cmd/agent/driver_linux.go + go mod tidy

**Files:**
- Modify: `cmd/agent/driver_linux.go`
- Run: `go mod tidy` (makes vishvananda/netlink a direct dependency)

**Step 1: No test needed** — this is pure wiring. Compilation is the test.

**Step 2: Modify `cmd/agent/driver_linux.go`**

```go
//go:build linux

package main

import (
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/syscode-labs/imp/internal/agent"
	"github.com/syscode-labs/imp/internal/agent/network"
)

// newProductionDriver creates a FirecrackerDriver wired with a LinuxNetManager.
// Reads FC_BIN, FC_SOCK_DIR, FC_KERNEL, FC_KERNEL_ARGS, and IMP_IMAGE_CACHE.
func newProductionDriver(client ctrlclient.Client) (agent.VMDriver, error) {
	d, err := agent.NewFirecrackerDriver(client)
	if err != nil {
		return nil, err
	}
	d.Net = network.NewLinuxNetManager()
	return d, nil
}
```

**Step 3: Run go mod tidy**

```bash
cd /Users/giovanni/syscode/git/imp
go mod tidy
```

Verify `go.mod` now shows `vishvananda/netlink` as a direct (non-indirect) dependency.

**Step 4: Build to verify compilation**

```bash
GOOS=linux go build ./cmd/agent/...
GOOS=linux go build ./internal/agent/...
go test ./internal/agent/network/...
go test ./internal/agent/...
```
Expected: all pass, no errors

**Step 5: Commit**

```bash
git add cmd/agent/driver_linux.go go.mod go.sum
git commit -m "feat(agent): wire LinuxNetManager into production driver"
```

---

## Test Command Reference

```bash
# Unit tests (portable, run on any OS):
go test ./internal/agent/network/...
go test ./internal/agent/...

# Cross-compile check (ensure linux-tagged files compile):
GOOS=linux go build ./...

# Full test suite:
go test ./...

# Integration tests (Linux root only — manual):
# sudo go test -tags integration ./internal/agent/network/...
```

## Notes

- **NAT reference counting**: Phase 1 does not tear down NAT rules when a VM stops (rules are shared across all VMs on the same network). Teardown would require reference counting. Acceptable for Phase 1 — add a TODO comment in `Stop`.
- **Recovery across agent restarts**: The in-memory `Allocator` is empty after restart. Running VMs' `status.IP` fields survive (stored in k8s), so the reconciler can call `Alloc.Reserve(netKey, vm.Status.IP)` during startup. This is a Phase 2 concern — add a TODO in `NewFirecrackerDriver`.
- **TAP persistence**: The Linux kernel TAP device persists across agent restarts (it's kernel state). On restart, the next reconcile of a running VM calls `Inspect` → finds no `fcProc` → returns `Running=false` → handles as VM exit. This is pre-existing behaviour (same as before networking). Phase 2 can add startup recovery.
- **DNS**: If `ImpNetwork.Spec.DNS` is empty, the `Nameservers` field in `IPConfiguration` will be nil. Firecracker/the kernel handles this gracefully (no `/proc/net/pnp` DNS entries, VM uses whatever is baked in).
