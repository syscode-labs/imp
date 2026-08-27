//go:build linux

package network

import (
	"context"
	"fmt"

	"github.com/vishvananda/netlink"
)

// EnsureLANBridge implements LANAttacher. See the interface for the contract.
func (m *LinuxNetManager) EnsureLANBridge(_ context.Context, bridgeName, parentIface string, vlanID int32) error {
	parent, err := netlink.LinkByName(parentIface)
	if err != nil {
		return fmt.Errorf("parent interface %s not found: %w", parentIface, err)
	}

	if vlanID == 0 {
		if _, ok := parent.(*netlink.Bridge); !ok {
			return fmt.Errorf("untagged attachment requires %s to be an existing bridge", parentIface)
		}
		return nil // TAPs attach to the administrator-managed bridge directly
	}

	vlanName := VLANIfaceName(parentIface, vlanID)
	vlanLink, err := netlink.LinkByName(vlanName)
	if err != nil {
		vlanLink = &netlink.Vlan{
			LinkAttrs: netlink.LinkAttrs{
				Name:        vlanName,
				ParentIndex: parent.Attrs().Index,
			},
			VlanId:       int(vlanID),
			VlanProtocol: netlink.VLAN_PROTOCOL_8021Q,
		}
		if err := netlink.LinkAdd(vlanLink); err != nil {
			return fmt.Errorf("create vlan subinterface %s: %w", vlanName, err)
		}
		vlanLink, err = netlink.LinkByName(vlanName)
		if err != nil {
			return fmt.Errorf("fetch vlan subinterface %s after create: %w", vlanName, err)
		}
	}

	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		br = &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}
		if err := netlink.LinkAdd(br); err != nil {
			return fmt.Errorf("create lan bridge %s: %w", bridgeName, err)
		}
		br, err = netlink.LinkByName(bridgeName)
		if err != nil {
			return fmt.Errorf("fetch lan bridge %s after create: %w", bridgeName, err)
		}
	}

	// Enslave the VLAN subinterface to the Imp bridge (idempotent when already master).
	if err := netlink.LinkSetMaster(vlanLink, br); err != nil {
		return fmt.Errorf("enslave %s to %s: %w", vlanName, bridgeName, err)
	}
	if err := netlink.LinkSetUp(vlanLink); err != nil {
		return fmt.Errorf("set %s up: %w", vlanName, err)
	}
	return netlink.LinkSetUp(br)
}

// TeardownLANBridgeIfUnused implements LANAttacher. See the interface.
func (m *LinuxNetManager) TeardownLANBridgeIfUnused(_ context.Context, bridgeName, parentIface string, vlanID int32) error {
	if vlanID == 0 {
		return nil // never touch administrator-managed bridges
	}

	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil // already gone — idempotent
	}

	ports, err := bridgedLinks(br)
	if err != nil {
		return fmt.Errorf("list ports of %s: %w", bridgeName, err)
	}
	if len(ports) > 0 {
		return nil // still in use by at least one VM TAP
	}

	if err := netlink.LinkDel(br); err != nil {
		return fmt.Errorf("delete unused lan bridge %s: %w", bridgeName, err)
	}

	vlanName := VLANIfaceName(parentIface, vlanID)
	vlanLink, err := netlink.LinkByName(vlanName)
	if err != nil {
		return nil // subinterface gone — idempotent
	}
	if err := netlink.LinkDel(vlanLink); err != nil {
		return fmt.Errorf("delete unused vlan subinterface %s: %w", vlanName, err)
	}
	return nil
}

// bridgedLinks returns links enslaved to br.
func bridgedLinks(br netlink.Link) ([]netlink.Link, error) {
	all, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	var ports []netlink.Link
	for _, l := range all {
		if l.Attrs().MasterIndex == br.Attrs().Index {
			ports = append(ports, l)
		}
	}
	return ports, nil
}

// compile-time assertions
var (
	_ NetManager  = (*LinuxNetManager)(nil)
	_ LANAttacher = (*LinuxNetManager)(nil)
)
