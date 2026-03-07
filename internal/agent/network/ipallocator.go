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
	allocated map[string]map[string]struct{} // netKey → set of allocated IPs
	vmCount   map[string]int                 // netKey → number of VMs currently holding an IP
}

// NewAllocator returns an empty Allocator.
func NewAllocator() *Allocator {
	return &Allocator{
		allocated: make(map[string]map[string]struct{}),
		vmCount:   make(map[string]int),
	}
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
				a.vmCount[netKey]++
				return s, nil
			}
		}
		ip = nextIP(ip)
	}
	return "", fmt.Errorf("no free IPs in subnet %s", subnet)
}

// Release frees a previously allocated IP so it can be reused.
// Returns true when this was the last allocated IP for netKey (VM count reached zero).
func (a *Allocator) Release(netKey, ip string) (wasLast bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if set, ok := a.allocated[netKey]; ok {
		delete(set, ip)
	}
	a.vmCount[netKey]--
	if a.vmCount[netKey] <= 0 {
		delete(a.vmCount, netKey)
		return true
	}
	return false
}

// Reserve marks an IP as in-use without going through Allocate.
// Use this during startup to re-register IPs from existing running VMs.
// It increments vmCount for netKey so that Release correctly tracks when
// the last VM for a network is removed.
func (a *Allocator) Reserve(netKey, ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.allocated[netKey] == nil {
		a.allocated[netKey] = make(map[string]struct{})
	}
	if _, alreadyReserved := a.allocated[netKey][ip]; !alreadyReserved {
		a.allocated[netKey][ip] = struct{}{}
		a.vmCount[netKey]++
	}
}

// sizeToCIDRPrefix returns the smallest CIDR prefix length that accommodates n hosts.
// Minimum is /30 (2 usable addresses). n=0 or n=1 returns /30.
// The result accounts for network and broadcast addresses (n+2 total addresses needed).
func sizeToCIDRPrefix(n int32) int {
	if n <= 2 {
		return 30
	}
	// Need n+2 addresses (network + broadcast)
	needed := int(n) + 2
	prefix := 30
	for (1 << (32 - prefix)) < needed {
		prefix--
	}
	return prefix
}

// nextIP returns a copy of ip with the value incremented by 1 (carries through octets).
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
