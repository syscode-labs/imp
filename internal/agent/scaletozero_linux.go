//go:build linux

package agent

import (
	"context"
	"net"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/types"
)

// resetIdle forgets any idle sample for key (called on suspend/resume so the VM
// starts a fresh idle window next time it runs). Only the linux reconciler uses it.
func (s *ScaleToZero) resetIdle(key types.NamespacedName) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.samples, key)
}

// activator runs the packet source, feeding matches into the wake registry. It
// runs on every node (no leader election) since each agent owns its own VMs.
type activator struct {
	src PacketSource
	reg *wakeRegistry
}

func (a *activator) Start(ctx context.Context) error {
	return a.src.Run(ctx, a.reg.onDstIP)
}

// NeedLeaderElection marks the activator as a per-node runnable.
func (a *activator) NeedLeaderElection() bool { return false }

// netlinkLinkStats returns cumulative rx+tx bytes for iface (a VM TAP), used by
// the idle detector to decide whether a ScaleToZero VM has gone quiet.
func netlinkLinkStats(iface string) (uint64, error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return 0, err
	}
	st := link.Attrs().Statistics
	if st == nil {
		return 0, nil
	}
	return st.RxBytes + st.TxBytes, nil
}

// afpacketSource captures inbound IPv4 frames on the node via a single AF_PACKET
// raw socket (unbound, so it sees every overlay bridge) and reports each frame's
// destination IP. One socket serves all suspended VMs on the node.
//
// UNVALIDATED (see scaletozero.go): not yet confirmed to observe the first frame
// destined to a TAP-less suspended VM. Swap for a tc-BPF PacketSource if the
// cluster spike shows the frame is dropped before this hook.
type afpacketSource struct{}

func htons(v uint16) uint16 { return v<<8 | v>>8 }

func (afpacketSource) Run(ctx context.Context, onDstIP func(string)) error {
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_IP)))
	if err != nil {
		return err
	}
	// Unblock the blocking Recvfrom and release the fd when the manager stops.
	go func() {
		<-ctx.Done()
		_ = unix.Close(fd)
	}()

	buf := make([]byte, 65536)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			if ctx.Err() != nil {
				return nil // closed on shutdown
			}
			time.Sleep(10 * time.Millisecond) // avoid a tight spin on a persistent recv error
			continue
		}
		// AF_PACKET/SOCK_RAW frames include the 14-byte Ethernet header; the IPv4
		// destination address sits at bytes 30..34 (eth[14] + ipv4[16..20]).
		if n < 34 {
			continue
		}
		onDstIP(net.IP(buf[30:34]).String())
	}
}

// NewLinuxScaleToZero wires the real host implementations into the neutral core.
func NewLinuxScaleToZero(bufSize int, interval time.Duration) *ScaleToZero {
	return newScaleToZero(netlinkLinkStats, afpacketSource{}, interval, bufSize)
}
