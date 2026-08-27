package network

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// maxIfaceNameLen is the Linux interface name limit (IFNAMSIZ - 1).
const maxIfaceNameLen = 15

// BridgeName returns a deterministic Linux bridge name for a network key
// (e.g. "default/mynet"). Always exactly 14 characters.
func BridgeName(netKey string) string {
	h := sha256.Sum256([]byte(netKey))
	return fmt.Sprintf("impbr-%08x", binary.BigEndian.Uint32(h[:4]))
}

// TAPName returns a deterministic TAP device name for a VM key
// (e.g. "default/my-vm"). Always exactly 15 characters.
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

// VLANIfaceName returns the 802.1Q subinterface name for a parent interface
// and VLAN ID. Uses "<parent>.<vid>" when it fits the kernel's 15-character
// interface-name limit, otherwise a deterministic short "impvl-" name so
// long parent names cannot collide across VLAN IDs.
func VLANIfaceName(parent string, vlanID int32) string {
	direct := fmt.Sprintf("%s.%d", parent, vlanID)
	if len(direct) <= maxIfaceNameLen {
		return direct
	}
	h := sha256.Sum256([]byte(direct))
	return fmt.Sprintf("impvl-%06x", binary.BigEndian.Uint32(h[:4])&0xffffff)
}
