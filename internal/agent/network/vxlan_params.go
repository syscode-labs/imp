package network

import (
	"crypto/sha256"
	"strings"
)

// VXLANParams returns the VNI and interface name for an ImpNetwork given its UID.
// VNI is derived from the first 3 bytes of the SHA-256 of the UID (24-bit, 1–16777215).
// Interface name is "impvx-<first 8 hex chars of UID>" (max 15 chars).
func VXLANParams(uid string) (vni uint32, ifaceName string) {
	h := sha256.Sum256([]byte(uid))
	vni = (uint32(h[0])<<16 | uint32(h[1])<<8 | uint32(h[2])) & 0xFFFFFF
	if vni == 0 {
		vni = 1 // avoid VNI 0
	}
	// UID has hyphens; strip them for a compact iface name.
	clean := strings.ReplaceAll(uid, "-", "")
	if len(clean) > 8 {
		clean = clean[:8]
	}
	ifaceName = "impvx-" + clean
	return
}
