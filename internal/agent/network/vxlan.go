//go:build linux

package network

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"github.com/vishvananda/netlink"
)

// EnsureVXLAN creates or reconciles the VXLAN interface for the given network,
// attaches it to bridgeName, and brings it up. bridgeName must already exist
// (call EnsureNetwork first). Idempotent.
func (m *LinuxNetManager) EnsureVXLAN(_ context.Context, vni uint32, ifaceName, nodeIP, bridgeName string) error {
	localIP := net.ParseIP(nodeIP)
	if localIP == nil {
		return fmt.Errorf("invalid nodeIP %q", nodeIP)
	}

	link, err := netlink.LinkByName(ifaceName)
	if err == nil {
		// Interface already exists — ensure it is up and attached to bridge.
		if err := netlink.LinkSetUp(link); err != nil {
			return err
		}
		return m.attachToBridge(link, bridgeName)
	}

	vx := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name: ifaceName,
		},
		VxlanId:  int(vni),
		SrcAddr:  localIP.To4(),
		Port:     8472,
		Learning: false,
		L2miss:   false,
		L3miss:   false,
	}
	if err := netlink.LinkAdd(vx); err != nil {
		return fmt.Errorf("create vxlan %s (vni %d): %w", ifaceName, vni, err)
	}

	link, err = netlink.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("fetch vxlan %s after create: %w", ifaceName, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return err
	}
	return m.attachToBridge(link, bridgeName)
}

// attachToBridge attaches link to the bridge named bridgeName.
// No-op if link is already a member of that bridge or bridgeName is empty.
func (m *LinuxNetManager) attachToBridge(link netlink.Link, bridgeName string) error {
	if bridgeName == "" {
		return nil
	}
	bridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("get bridge %s: %w", bridgeName, err)
	}
	// Already a member?
	if link.Attrs().MasterIndex == bridge.Attrs().Index {
		return nil
	}
	return netlink.LinkSetMaster(link, bridge)
}

// SyncFDB reconciles the local FDB (forwarding database) on the VXLAN interface
// to match the provided entries. Entries not in the list are removed.
// Idempotent.
func (m *LinuxNetManager) SyncFDB(_ context.Context, ifaceName string, entries []FDBEntry) error {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("get vxlan interface %s: %w", ifaceName, err)
	}

	// Build desired set (MAC → DstIP).
	desired := make(map[string]string, len(entries))
	for _, e := range entries {
		desired[e.MAC] = e.DstIP
	}

	// List current FDB entries.
	current, err := netlink.NeighList(link.Attrs().Index, syscall.AF_BRIDGE)
	if err != nil {
		return fmt.Errorf("list FDB entries for %s: %w", ifaceName, err)
	}

	// Remove stale entries — skip the all-zeros broadcast entry.
	allZeros := "00:00:00:00:00:00"
	for _, n := range current {
		mac := n.HardwareAddr.String()
		if mac == allZeros {
			continue
		}
		if _, ok := desired[mac]; !ok {
			del := &netlink.Neigh{
				LinkIndex:    link.Attrs().Index,
				State:        netlink.NUD_PERMANENT,
				Family:       syscall.AF_BRIDGE,
				Flags:        netlink.NTF_SELF,
				HardwareAddr: n.HardwareAddr,
				IP:           n.IP,
			}
			if err := netlink.NeighDel(del); err != nil {
				return fmt.Errorf("delete FDB entry %s on %s: %w", mac, ifaceName, err)
			}
		}
	}

	// Build set of existing MACs for quick lookup.
	existing := make(map[string]struct{}, len(current))
	for _, n := range current {
		existing[n.HardwareAddr.String()] = struct{}{}
	}

	// Add missing entries.
	for mac, dstIP := range desired {
		if _, ok := existing[mac]; ok {
			continue
		}
		hwAddr, err := net.ParseMAC(mac)
		if err != nil {
			return fmt.Errorf("parse MAC %q: %w", mac, err)
		}
		dst := net.ParseIP(dstIP)
		if dst == nil {
			return fmt.Errorf("invalid FDB dst IP %q", dstIP)
		}
		add := &netlink.Neigh{
			LinkIndex:    link.Attrs().Index,
			State:        netlink.NUD_PERMANENT,
			Family:       syscall.AF_BRIDGE,
			Flags:        netlink.NTF_SELF,
			HardwareAddr: hwAddr,
			IP:           dst,
		}
		if err := netlink.NeighAdd(add); err != nil {
			return fmt.Errorf("add FDB entry %s→%s on %s: %w", mac, dstIP, ifaceName, err)
		}
	}

	return nil
}
