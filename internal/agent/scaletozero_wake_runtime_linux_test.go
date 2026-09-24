//go:build linux

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/runtimeapi"
)

func TestScaleToZeroRuntimeWakeSourceWakesOnRuntimeObservedFrame(t *testing.T) {
	t.Parallel()

	const overlayIP = "10.45.0.3"
	hits := [][]string{
		{"10.244.0.8", "10.96.0.1"},         // poll 1: pod traffic only — no match
		{overlayIP, "127.0.0.1", overlayIP}, // poll 2: the wake frame arrives
	}
	poll := 0
	server := runtimeapi.NewServer(runtimeapi.BackendFuncs{
		LinkStatsFunc: func(_ context.Context, _ string) (uint64, error) {
			return 42, nil // constant: observe() never reports idle
		},
		WakeHitsFunc: func(_ context.Context) ([]string, error) {
			if poll >= len(hits) {
				return nil, nil
			}
			got := hits[poll]
			poll++
			return got, nil
		},
	})
	dir, err := os.MkdirTemp("/tmp", "imp-runtime-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	endpoint := filepath.Join(dir, "runtime.sock")
	if err := server.Start(endpoint); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	sz := NewLinuxScaleToZeroWithRuntime(runtimeapi.NewClient(endpoint), 1, time.Second)
	key := types.NamespacedName{Namespace: "default", Name: "target"}

	// Register the VM as suspended+awaiting wake, exactly as the reconciler
	// does when it parks a ScaleToZero VM.
	sz.reg.register(overlayIP, &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
	})

	// Feed the first poll through the wake registry directly: pod traffic
	// must not signal the suspended VM.
	_, _, err = sz.observe(key, "imptap-x", time.Minute, time.Now())
	if err != nil {
		t.Fatalf("observe() error = %v", err)
	}
	// Drain hits via the same path the activator uses.
	src := runtimeWakeSource{client: runtimeapi.NewClient(endpoint), poll: 50 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = src.Run(ctx, sz.reg.onDstIP)
		close(done)
	}()

	// Wait until the second poll (which contains the overlay IP) has been
	// consumed — the wake registry must have signalled the VM.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sz.reg.mu.Lock()
		signalled := sz.reg.signalled[key]
		sz.reg.mu.Unlock()
		if signalled {
			cancel()
			<-done
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("wake registry never signalled the VM despite the runtime observing a frame for its overlay IP")
}
