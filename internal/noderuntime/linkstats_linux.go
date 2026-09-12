//go:build linux

package noderuntime

import (
	"context"

	"github.com/vishvananda/netlink"
)

// runtimeLinkStats reads the TAP from the runtime's network namespace. The
// agent must use the RPC instead of attempting this lookup in its own namespace.
func runtimeLinkStats(_ context.Context, tapName string) (uint64, error) {
	link, err := netlink.LinkByName(tapName)
	if err != nil {
		return 0, err
	}
	stats := link.Attrs().Statistics
	if stats == nil {
		return 0, nil
	}
	return stats.RxBytes + stats.TxBytes, nil
}
