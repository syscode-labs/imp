package agent

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func sztVM(ns, name string) *impdevv1alpha1.ImpVM {
	return &impdevv1alpha1.ImpVM{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func TestWakeRegistry_MatchAndDedup(t *testing.T) {
	reg := newWakeRegistry(8)
	vm := sztVM("ns", "web")
	key := client.ObjectKeyFromObject(vm)
	reg.register("10.0.0.5", vm)

	reg.onDstIP("10.0.0.5")
	select {
	case ev := <-reg.events:
		if ev.Object.GetName() != "web" {
			t.Fatalf("event for wrong object: %s", ev.Object.GetName())
		}
	default:
		t.Fatal("expected a wake event, got none")
	}
	if !reg.pending(key) {
		t.Fatal("expected pending after wake")
	}

	// Second packet for the same VM must not enqueue a duplicate.
	reg.onDstIP("10.0.0.5")
	select {
	case <-reg.events:
		t.Fatal("expected dedup, got a second event")
	default:
	}
}

func TestWakeRegistry_UnknownIP(t *testing.T) {
	reg := newWakeRegistry(8)
	reg.register("10.0.0.5", sztVM("ns", "web"))
	reg.onDstIP("10.0.0.9") // not registered
	select {
	case <-reg.events:
		t.Fatal("unexpected event for unregistered IP")
	default:
	}
}

func TestWakeRegistry_Clear(t *testing.T) {
	reg := newWakeRegistry(8)
	vm := sztVM("ns", "web")
	key := client.ObjectKeyFromObject(vm)
	reg.register("10.0.0.5", vm)
	reg.onDstIP("10.0.0.5")
	<-reg.events

	reg.clear(key)
	if reg.pending(key) {
		t.Fatal("pending should be false after clear")
	}
	// IP mapping is gone: a later packet must not wake.
	reg.onDstIP("10.0.0.5")
	select {
	case <-reg.events:
		t.Fatal("cleared IP should not wake")
	default:
	}
}

// A full channel must not lose a wake: signalled stays false so the next packet
// retries once there is room.
func TestWakeRegistry_FullChannelRetries(t *testing.T) {
	reg := newWakeRegistry(1)
	a, b := sztVM("ns", "a"), sztVM("ns", "b")
	reg.register("10.0.0.1", a)
	reg.register("10.0.0.2", b)

	reg.onDstIP("10.0.0.1") // fills the buffer of 1
	reg.onDstIP("10.0.0.2") // channel full → dropped, not signalled
	if reg.pending(client.ObjectKeyFromObject(b)) {
		t.Fatal("b must not be signalled while channel is full")
	}

	<-reg.events            // drain a's event
	reg.onDstIP("10.0.0.2") // retry now succeeds
	if !reg.pending(client.ObjectKeyFromObject(b)) {
		t.Fatal("b should be signalled after retry")
	}
}

func TestWakeRegistry_ReregisterDropsOldIP(t *testing.T) {
	reg := newWakeRegistry(8)
	vm := sztVM("ns", "web")
	reg.register("10.0.0.5", vm)
	reg.register("10.0.0.6", vm) // VM came back with a new IP

	reg.onDstIP("10.0.0.5") // stale IP must not wake
	select {
	case <-reg.events:
		t.Fatal("stale IP should have been dropped on re-register")
	default:
	}
	reg.onDstIP("10.0.0.6")
	if !reg.pending(client.ObjectKeyFromObject(vm)) {
		t.Fatal("new IP should wake")
	}
}

func TestObserve_IdleAfterTimeout(t *testing.T) {
	var bytes uint64 = 100
	sz := newScaleToZero(func(string) (uint64, error) { return bytes, nil }, nil, time.Second, 8)
	key := client.ObjectKeyFromObject(sztVM("ns", "web"))
	t0 := time.Now()

	if idle, _, _ := sz.observe(key, "tap0", time.Minute, t0); idle {
		t.Fatal("first sample must never be idle (grace window)")
	}
	// Same byte count, within timeout → still not idle.
	if idle, _, _ := sz.observe(key, "tap0", time.Minute, t0.Add(30*time.Second)); idle {
		t.Fatal("within idleTimeout should not be idle")
	}
	// Same byte count, past timeout → idle.
	if idle, _, _ := sz.observe(key, "tap0", time.Minute, t0.Add(90*time.Second)); !idle {
		t.Fatal("past idleTimeout with no traffic should be idle")
	}
}

func TestObserve_TrafficResetsClock(t *testing.T) {
	var bytes uint64 = 100
	sz := newScaleToZero(func(string) (uint64, error) { return bytes, nil }, nil, time.Second, 8)
	key := client.ObjectKeyFromObject(sztVM("ns", "web"))
	t0 := time.Now()

	sz.observe(key, "tap0", time.Minute, t0)
	bytes = 500 // traffic happened
	idle, lastActivity, _ := sz.observe(key, "tap0", time.Minute, t0.Add(90*time.Second))
	if idle {
		t.Fatal("traffic must reset the idle clock")
	}
	if !lastActivity.Equal(t0.Add(90 * time.Second)) {
		t.Fatalf("lastActivity should be the traffic time, got %v", lastActivity)
	}
	// Now idle from the reset point.
	if idle, _, _ := sz.observe(key, "tap0", time.Minute, t0.Add(90*time.Second).Add(2*time.Minute)); !idle {
		t.Fatal("should be idle a full timeout after the reset")
	}
}

func TestIdleTimeoutOrDefault(t *testing.T) {
	if got := idleTimeoutOrDefault(sztVM("ns", "a")); got != defaultIdleTimeout {
		t.Errorf("unset: got %v, want %v", got, defaultIdleTimeout)
	}
	vm := sztVM("ns", "a")
	vm.Spec.IdleTimeout = &metav1.Duration{Duration: 2 * time.Minute}
	if got := idleTimeoutOrDefault(vm); got != 2*time.Minute {
		t.Errorf("set: got %v, want 2m", got)
	}
}

// Run with -race: concurrent activator (onDstIP) vs reconcile (register/clear/pending).
func TestWakeRegistry_ConcurrentAccess(t *testing.T) {
	reg := newWakeRegistry(1024)
	vm := sztVM("ns", "web")
	key := client.ObjectKeyFromObject(vm)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			reg.onDstIP("10.0.0.5")
		}
		close(done)
	}()
	for i := 0; i < 2000; i++ {
		reg.register("10.0.0.5", vm)
		reg.pending(key)
		reg.clear(key)
	}
	<-done
}
