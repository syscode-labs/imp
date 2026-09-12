//go:build linux

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/syscode-labs/imp/internal/runtimeapi"
)

func TestScaleToZeroRuntimeLinkStatsUsesRuntimeUnixSocket(t *testing.T) {
	t.Parallel()

	const wantTAP = "imptap-scale-to-zero"
	const wantBytes = uint64(1234)
	server := runtimeapi.NewServer(runtimeapi.BackendFuncs{
		LinkStatsFunc: func(_ context.Context, tapName string) (uint64, error) {
			if tapName != wantTAP {
				t.Errorf("tapName = %q, want %q", tapName, wantTAP)
			}
			return wantBytes, nil
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
	key := types.NamespacedName{Namespace: "default", Name: "worker"}
	t0 := time.Now()
	idle, _, err := sz.observe(key, wantTAP, time.Minute, t0)
	if err != nil {
		t.Fatalf("first observe() error = %v", err)
	}
	if idle {
		t.Fatal("first observe() reported idle")
	}
	idle, _, err = sz.observe(key, wantTAP, time.Minute, t0.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("second observe() error = %v", err)
	}
	if !idle {
		t.Fatal("second observe() did not use the unchanged runtime byte count")
	}
}
