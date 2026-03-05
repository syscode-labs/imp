//go:build linux

package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

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
		// Bridge already exists — ensure gateway IP is assigned and link is up.
		addr := &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   net.ParseIP(gatewayIP).To4(),
				Mask: net.CIDRMask(prefixLen, 32),
			},
		}
		if addErr := netlink.AddrAdd(link, addr); addErr != nil && !errors.Is(addErr, os.ErrExist) {
			return fmt.Errorf("assign %s/%d to existing bridge %s: %w", gatewayIP, prefixLen, bridgeName, addErr)
		}
		return netlink.LinkSetUp(link)
	}

	br := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}
	if err := netlink.LinkAdd(br); err != nil {
		return fmt.Errorf("create bridge %s: %w", bridgeName, err)
	}

	// Re-fetch link after creation to get updated attrs.
	link, err = netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("fetch bridge %s after create: %w", bridgeName, err)
	}

	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP(gatewayIP).To4(),
			Mask: net.CIDRMask(prefixLen, 32),
		},
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("assign %s/%d to bridge %s: %w", gatewayIP, prefixLen, bridgeName, err)
	}
	return netlink.LinkSetUp(link)
}

// SetupVM creates TAP device tapName, attaches it to bridgeName, and sets it up.
func (m *LinuxNetManager) SetupVM(_ context.Context, tapName, bridgeName, _ string) error {
	// Check if TAP already exists; create it if not.
	tapLink, err := netlink.LinkByName(tapName)
	if err != nil {
		tap := &netlink.Tuntap{
			LinkAttrs: netlink.LinkAttrs{Name: tapName},
			Mode:      netlink.TUNTAP_MODE_TAP,
		}
		if err := netlink.LinkAdd(tap); err != nil {
			return fmt.Errorf("create tap %s: %w", tapName, err)
		}
		tapLink, err = netlink.LinkByName(tapName)
		if err != nil {
			return fmt.Errorf("get tap %s after create: %w", tapName, err)
		}
	}

	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("get bridge %s: %w", bridgeName, err)
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
		// Device not found — already gone, treat as success.
		return nil
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete tap %s: %w", tapName, err)
	}
	return nil
}

// EnsureNAT installs a MASQUERADE rule for subnet on egressIface.
// If egressIface is empty, the default-route interface is auto-detected.
// Uses nftables if available, otherwise iptables. Idempotent.
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

// defaultRouteIface returns the network interface on the default IPv4 route.
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
func ensureNATNftables(subnet, egressIface string) error {
	_ = exec.Command("nft", "add", "table", "ip", "imp_nat").Run()                                                               //nolint:errcheck,gosec
	_ = exec.Command("nft", "add", "chain", "ip", "imp_nat", "postrouting", "{ type nat hook postrouting priority 100; }").Run() //nolint:errcheck,gosec

	// Check whether a rule for this subnet is already present.
	//nolint:gosec
	if out, err := exec.Command("nft", "list", "chain", "ip", "imp_nat", "postrouting").Output(); err == nil {
		if strings.Contains(string(out), subnet) {
			return nil // rule already installed
		}
	}

	rule := fmt.Sprintf("ip saddr %s oifname %q masquerade", subnet, egressIface)
	//nolint:gosec // G204: subnet and egressIface are controlled values
	if out, err := exec.Command("nft", "add", "rule", "ip", "imp_nat", "postrouting", rule).CombinedOutput(); err != nil {
		return fmt.Errorf("nft add rule: %w: %s", err, out)
	}
	return nil
}

// ensureNATIptables adds a MASQUERADE rule via iptables (skips if already present).
func ensureNATIptables(subnet, egressIface string) error {
	// -C checks for existence — if it succeeds the rule is already installed.
	//nolint:gosec
	check := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", "-s", subnet, "-o", egressIface, "-j", "MASQUERADE")
	if check.Run() == nil {
		return nil
	}
	//nolint:gosec // G204: inputs are controlled values
	out, err := exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", subnet, "-o", egressIface, "-j", "MASQUERADE").CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables: %w: %s", err, out)
	}
	return nil
}

// RemoveNAT removes the MASQUERADE rule for subnet on egressIface.
// Idempotent — no-op if the rule does not exist. Uses the same backend
// (nftables or iptables) chosen at construction time.
func (m *LinuxNetManager) RemoveNAT(_ context.Context, subnet, egressIface string) error {
	if egressIface == "" {
		iface, err := defaultRouteIface()
		if err != nil {
			return fmt.Errorf("detect egress interface: %w", err)
		}
		egressIface = iface
	}
	if m.natBackend == "nftables" {
		return removeNATNftables(subnet, egressIface)
	}
	return removeNATIptables(subnet, egressIface)
}

// removeNATNftables removes the per-subnet MASQUERADE rule from the imp_nat chain.
// Uses handle-based deletion to avoid removing unrelated rules.
func removeNATNftables(subnet, _ string) error {
	//nolint:gosec
	out, err := exec.Command("nft", "-a", "list", "chain", "ip", "imp_nat", "postrouting").Output()
	if err != nil {
		return nil // chain doesn't exist — idempotent
	}
	handle := findNftHandle(string(out), subnet)
	if handle == "" {
		return nil // rule not found — idempotent
	}
	//nolint:gosec // G204: handle is a number parsed from nft output
	if out2, err := exec.Command("nft", "delete", "rule", "ip", "imp_nat", "postrouting", "handle", handle).CombinedOutput(); err != nil {
		return fmt.Errorf("nft delete rule: %w: %s", err, out2)
	}
	return nil
}

// findNftHandle returns the handle number for the first rule in nft -a list output
// that contains subnet. Returns "" if not found.
func findNftHandle(output, subnet string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, subnet) {
			if idx := strings.Index(line, "# handle "); idx >= 0 {
				rest := strings.TrimSpace(line[idx+len("# handle "):])
				if fields := strings.Fields(rest); len(fields) > 0 {
					return fields[0]
				}
			}
		}
	}
	return ""
}

// removeNATIptables deletes the MASQUERADE rule via iptables -D.
// Idempotent: treats "not found" (exit 1) as success.
func removeNATIptables(subnet, egressIface string) error {
	// -C checks whether the rule exists. Non-zero exit means not found — nothing to remove.
	//nolint:gosec // G204: subnet and egressIface are controlled values
	check := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING",
		"-s", subnet, "-o", egressIface, "-j", "MASQUERADE")
	if check.Run() != nil {
		return nil // rule not present — idempotent
	}
	//nolint:gosec // G204: subnet and egressIface are controlled values
	if out, err := exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING",
		"-s", subnet, "-o", egressIface, "-j", "MASQUERADE").CombinedOutput(); err != nil {
		return fmt.Errorf("iptables -D: %w: %s", err, out)
	}
	return nil
}

// compile-time assertion
var _ NetManager = (*LinuxNetManager)(nil)
