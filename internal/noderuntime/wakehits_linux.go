//go:build linux

package noderuntime

import (
	"context"
	"net"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// wakeHits records destination IPs of inbound IPv4 frames observed in the
// runtime's network namespace (the host netns, where the impbr-* bridges and
// VM TAPs live). The agent polls WakeHits and wakes any suspended ScaleToZero
// VM whose overlay IP appears in the drained set.
type wakeHits struct {
	mu   sync.Mutex
	hits map[string]bool
}

func (w *wakeHits) add(ip string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hits[ip] = true
}

func (w *wakeHits) take() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	ips := make([]string, 0, len(w.hits))
	for ip := range w.hits {
		ips = append(ips, ip)
	}
	w.hits = map[string]bool{}
	return ips
}

func htons16(v uint16) uint16 { return v<<8 | v>>8 }

// startHostWakeCapture opens one AF_PACKET socket bound to ETH_P_ALL in the
// runtime's network namespace and records each inbound IPv4 frame's
// destination IP into hits until ctx is cancelled.
//
// The socket MUST use ETH_P_ALL rather than ETH_P_IP: a specific EtherType
// registers in the kernel's ptype_base hash, which a bridged device's
// rx_handler (br_handle_frame, RX_HANDLER_CONSUMED) never reaches. Only
// ptype_all (protocol 0) is consulted before the rx_handler, so it is the only
// way to see frames switched onto an impbr-* bridge. IPv4 filtering therefore
// happens in userspace. Do not "simplify" this back to ETH_P_IP — it silently
// stops seeing all bridged VM traffic (see the longer analysis in
// internal/agent/scaletozero_linux.go and imp#35).
func startHostWakeCapture(ctx context.Context, hits *wakeHits) {
	log := logf.Log.WithName("wake-capture")
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons16(unix.ETH_P_ALL)))
	if err != nil {
		log.Error(err, "wake-capture: failed to open AF_PACKET socket in host netns; wake-on-traffic is unavailable on this node")
		return
	}
	log.Info("wake-capture: AF_PACKET socket open in host netns, capturing inbound IPv4 frames")
	go func() {
		<-ctx.Done()
		_ = unix.Close(fd)
	}()
	buf := make([]byte, 65536)
	go func() {
		for {
			n, _, err := unix.Recvfrom(fd, buf, 0)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			// Frames include the 14-byte Ethernet header; filter to IPv4 via
			// the EtherType at bytes 12..14 and read the destination IPv4
			// address at bytes 30..34 (eth[14] + ipv4[16..20]).
			if n < 34 {
				continue
			}
			if ethType := uint16(buf[12])<<8 | uint16(buf[13]); ethType != 0x0800 {
				continue
			}
			hits.add(net.IP(buf[30:34]).String())
		}
	}()
}

// backendWakeState holds the lazy capture state that only exists on Linux.
type backendWakeState struct {
	hits   *wakeHits
	cancel context.CancelFunc
}

// wakeStateOf lazily initialises b's wake capture on first poll.
func wakeStateOf(b *Backend) *backendWakeState {
	if state, ok := b.wakeHitsState.(*backendWakeState); ok {
		return state
	}
	state := &backendWakeState{hits: &wakeHits{hits: map[string]bool{}}}
	// The capture intentionally lives for the whole runtime process; it is
	// never cancelled short of process exit, so the cancel func stays stored
	// but uncalled.
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // intentional process-lifetime capture
	state.cancel = cancel
	startHostWakeCapture(ctx, state.hits)
	b.wakeHitsState = state
	return state
}

// WakeHits drains the destination IPs observed on the host since the previous
// call. Implements runtimeapi.Backend.
func (b *Backend) WakeHits(_ context.Context) ([]string, error) {
	if b.WakeHitsFunc != nil {
		return b.WakeHitsFunc(context.Background())
	}
	return wakeStateOf(b).hits.take(), nil
}
