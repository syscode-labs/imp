package agent

// Scale-to-zero wake-on-traffic support (Phase 3).
//
// This file holds the platform-neutral, fully unit-tested core: the wake
// registry (which suspended VMs are awaiting a packet), the traffic-idle
// detector, and the reconcile-triggering plumbing. The two pieces that need a
// real host — reading NIC byte counters and capturing packets — are injected as
// a linkStatsFunc and a PacketSource, faked in tests and implemented for real in
// scaletozero_linux.go.
//
// UNVALIDATED: the real PacketSource (AF_PACKET on the overlay) has NOT been
// confirmed to observe the first frame destined to a TAP-less (suspended) VM.
// The first cluster spike (Phase 1 of the wake-on-traffic plan) must validate
// hook placement. The PacketSource interface exists precisely so the hook can be
// swapped to tc-BPF later without touching any of the logic below.

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// defaultIdleTimeout is used when a ScaleToZero VM leaves spec.idleTimeout unset.
const defaultIdleTimeout = 5 * time.Minute

// PacketSource delivers the destination IP of each inbound overlay frame to
// onDstIP until ctx is cancelled. Implementations are host-specific.
type PacketSource interface {
	Run(ctx context.Context, onDstIP func(ip string)) error
}

// linkStatsFunc returns cumulative rx+tx bytes for a host interface (a VM TAP).
type linkStatsFunc func(iface string) (bytes uint64, err error)

// wakeRegistry tracks suspended ScaleToZero VMs by IP and fires a reconcile when
// a packet arrives for one. Safe for concurrent use: the activator goroutine
// calls onDstIP while the reconcile loop calls register/clear/pending.
type wakeRegistry struct {
	mu        sync.Mutex
	keyByIP   map[string]types.NamespacedName
	ipByKey   map[types.NamespacedName]string
	objByKey  map[types.NamespacedName]client.Object
	signalled map[types.NamespacedName]bool
	events    chan event.GenericEvent
}

func newWakeRegistry(bufSize int) *wakeRegistry {
	if bufSize <= 0 {
		bufSize = 1024
	}
	return &wakeRegistry{
		keyByIP:   map[string]types.NamespacedName{},
		ipByKey:   map[types.NamespacedName]string{},
		objByKey:  map[types.NamespacedName]client.Object{},
		signalled: map[types.NamespacedName]bool{},
		events:    make(chan event.GenericEvent, bufSize),
	}
}

// register marks vm as suspended and awaiting a wake packet at ip.
func (w *wakeRegistry) register(ip string, vm client.Object) {
	key := client.ObjectKeyFromObject(vm)
	w.mu.Lock()
	defer w.mu.Unlock()
	// Drop any stale IP mapping for this VM before recording the new one.
	if old, ok := w.ipByKey[key]; ok {
		delete(w.keyByIP, old)
	}
	w.keyByIP[ip] = key
	w.ipByKey[key] = ip
	w.objByKey[key] = vm
}

// onDstIP is the PacketSource callback: a frame arrived for ip. If ip belongs to
// a registered VM not already signalled, enqueue a reconcile for it. The
// signalled flag is set only when the event is actually enqueued, so a full
// channel never silently loses a wake — the next packet retries.
func (w *wakeRegistry) onDstIP(ip string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key, ok := w.keyByIP[ip]
	if !ok || w.signalled[key] {
		return
	}
	obj := w.objByKey[key]
	select {
	case w.events <- event.GenericEvent{Object: obj}:
		w.signalled[key] = true
	default:
		// Channel full; leave unsignalled so a later packet retries.
	}
}

// pending reports whether a wake packet has been observed for key.
func (w *wakeRegistry) pending(key types.NamespacedName) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.signalled[key]
}

// clear drops all state for key (called once the VM has resumed).
func (w *wakeRegistry) clear(key types.NamespacedName) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ip, ok := w.ipByKey[key]; ok {
		delete(w.keyByIP, ip)
	}
	delete(w.ipByKey, key)
	delete(w.objByKey, key)
	delete(w.signalled, key)
}

// ScaleToZero bundles the wake registry, the idle detector, and the packet
// source into the optional feature attached to the reconciler as SZ.
type ScaleToZero struct {
	reg      *wakeRegistry
	stats    linkStatsFunc
	src      PacketSource
	interval time.Duration

	mu      sync.Mutex
	samples map[types.NamespacedName]idleSample
}

type idleSample struct {
	bytes uint64
	since time.Time
}

func newScaleToZero(stats linkStatsFunc, src PacketSource, interval time.Duration, bufSize int) *ScaleToZero {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &ScaleToZero{
		reg:      newWakeRegistry(bufSize),
		stats:    stats,
		src:      src,
		interval: interval,
		samples:  map[types.NamespacedName]idleSample{},
	}
}

// observe samples iface's byte counter and reports whether the VM has seen no
// traffic for at least idleTimeout. Any change resets the idle clock, so a
// freshly-resumed VM (no prior sample) always gets a full idleTimeout of grace —
// this is the anti-thrash hysteresis.
//
// ASSUMPTION (validate on cluster): byte-idle suspends a VM holding an idle-but-
// open connection (long-poll, pooled DB conn), which the resume must transparently
// re-establish or the connection breaks. Combining with the guest CPU-idle signal
// is an open design question; not built here.
func (s *ScaleToZero) observe(key types.NamespacedName, iface string, idleTimeout time.Duration, now time.Time) (idle bool, lastActivity time.Time, err error) {
	b, err := s.stats(iface)
	if err != nil {
		return false, time.Time{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.samples[key]
	if !ok || b != prev.bytes {
		s.samples[key] = idleSample{bytes: b, since: now}
		return false, now, nil
	}
	return now.Sub(prev.since) >= idleTimeout, prev.since, nil
}

// idleTimeoutOrDefault resolves the effective idle window for a VM.
func idleTimeoutOrDefault(vm *impdevv1alpha1.ImpVM) time.Duration {
	if vm.Spec.IdleTimeout != nil && vm.Spec.IdleTimeout.Duration > 0 {
		return vm.Spec.IdleTimeout.Duration
	}
	return defaultIdleTimeout
}
