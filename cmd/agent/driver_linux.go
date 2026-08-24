//go:build linux

package main

import (
	"os"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/syscode-labs/imp/internal/agent"
	"github.com/syscode-labs/imp/internal/agent/network"
)

// newProductionDriver creates a FirecrackerDriver wired with a LinuxNetManager.
// Reads FC_BIN, FC_SOCK_DIR, FC_KERNEL, FC_KERNEL_ARGS, and IMP_IMAGE_CACHE.
// Returns the driver, the shared NetManager, and any error.
func newProductionDriver(_ ctrlclient.Client, _ *agent.VMMetricsCollector, _ string) (agent.VMDriver, network.NetManager, error) {
	endpoint := os.Getenv("IMP_RUNTIME_SOCKET")
	if endpoint == "" {
		endpoint = "/run/imp/runtime.sock"
	}
	return agent.NewRuntimeDriver(endpoint), agent.NewRuntimeNetManager(endpoint), nil
}
