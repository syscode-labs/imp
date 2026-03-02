package network

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

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
