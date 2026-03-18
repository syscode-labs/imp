package controller

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// carveGroupCIDRs derives one subnet per group from parentCIDR.
// Groups are sorted alphabetically for deterministic output.
// Each group gets the smallest subnet that fits ExpectedSize hosts
// (minimum /30 for 0 or 1 host). Subnets are placed consecutively,
// aligned to their natural block boundary.
// Returns an error if the parent subnet is exhausted.
func carveGroupCIDRs(parentCIDR string, groups []impdevv1alpha1.NetworkGroupSpec) ([]impdevv1alpha1.GroupCIDR, error) {
	if len(groups) == 0 {
		return nil, nil
	}

	_, parent, err := net.ParseCIDR(parentCIDR)
	if err != nil {
		return nil, fmt.Errorf("parse subnet %q: %w", parentCIDR, err)
	}
	parentStart := ipToUint32(parent.IP.To4())
	parentPrefix, _ := parent.Mask.Size()
	parentSize := uint32(1) << (32 - parentPrefix)

	// Sort groups by name for determinism.
	sorted := make([]impdevv1alpha1.NetworkGroupSpec, len(groups))
	copy(sorted, groups)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	result := make([]impdevv1alpha1.GroupCIDR, 0, len(sorted))
	cursor := parentStart

	for _, g := range sorted {
		prefix := groupPrefixLen(g.ExpectedSize)
		blockSize := uint32(1) << (32 - prefix)

		// Align cursor to block boundary.
		aligned := (cursor + blockSize - 1) &^ (blockSize - 1)
		if aligned < parentStart || aligned-parentStart+blockSize > parentSize {
			return nil, fmt.Errorf("group %q: no space left in %s", g.Name, parentCIDR)
		}

		ip := uint32ToIP(aligned)
		result = append(result, impdevv1alpha1.GroupCIDR{
			Name: g.Name,
			CIDR: fmt.Sprintf("%s/%d", ip.String(), prefix),
		})
		cursor = aligned + blockSize
	}
	return result, nil
}

// groupPrefixLen returns the smallest CIDR prefix that fits n hosts.
// Minimum is /30 (2 usable addresses). Identical semantics to
// network.sizeToCIDRPrefix in the agent package.
func groupPrefixLen(n int32) int {
	if n <= 2 {
		return 30
	}
	needed := int(n) + 2 // +2 for network and broadcast
	prefix := 30
	for (1 << (32 - prefix)) < needed {
		prefix--
	}
	return prefix
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return binary.BigEndian.Uint32(ip)
}

func uint32ToIP(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}
