package runtimeapi_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/runtimeapi"
)

func TestClientGetReturnsRuntimeState(t *testing.T) {
	t.Parallel()

	vm := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "worker", UID: types.UID("vm-uid")},
	}
	server := runtimeapi.NewServer(runtimeapi.BackendFuncs{
		GetFunc: func(context.Context, *impdevv1alpha1.ImpVM) (runtimeapi.VMState, error) {
			return runtimeapi.VMState{Running: true, PID: 42, IP: "10.0.0.2"}, nil
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

	client := runtimeapi.NewClient(endpoint)
	state, err := client.Get(context.Background(), vm)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !state.Running || state.PID != 42 || state.IP != "10.0.0.2" {
		t.Fatalf("Get() = %#v, want running VM state", state)
	}
}

func TestClientEnsureNetworkForwardsHostNetworkRequest(t *testing.T) {
	t.Parallel()
	called := make(chan string, 1)
	server := runtimeapi.NewServer(runtimeapi.BackendFuncs{
		EnsureNetworkFunc: func(_ context.Context, bridge, gateway string, prefix int) error {
			called <- bridge + ":" + gateway + ":" + strconv.Itoa(prefix)
			return nil
		},
	})
	dir, err := os.MkdirTemp("/tmp", "imp-runtime-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := server.Start(filepath.Join(dir, "runtime.sock")); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	if err := runtimeapi.NewClient(filepath.Join(dir, "runtime.sock")).EnsureNetwork(context.Background(), "impbr-test", "10.1.0.1", 24); err != nil {
		t.Fatalf("EnsureNetwork() error = %v", err)
	}
	select {
	case got := <-called:
		if got != "impbr-test:10.1.0.1:24" {
			t.Fatalf("EnsureNetwork() = %q", got)
		}
	default:
		t.Fatal("EnsureNetwork() did not reach runtime")
	}
}
