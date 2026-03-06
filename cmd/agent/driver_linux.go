//go:build linux

package main

import (
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/syscode-labs/imp/internal/agent"
	"github.com/syscode-labs/imp/internal/agent/network"
)

// newProductionDriver creates a FirecrackerDriver wired with a LinuxNetManager.
// Reads FC_BIN, FC_SOCK_DIR, FC_KERNEL, FC_KERNEL_ARGS, and IMP_IMAGE_CACHE.
// Returns the driver, the shared NetManager, and any error.
func newProductionDriver(client ctrlclient.Client, mc *agent.VMMetricsCollector, nodeName string) (agent.VMDriver, network.NetManager, error) {
	d, err := agent.NewFirecrackerDriver(client)
	if err != nil {
		return nil, nil, err
	}
	nm := network.NewLinuxNetManager()
	d.Net = nm
	d.Metrics = mc
	d.NodeName = nodeName
	return d, nm, nil
}
