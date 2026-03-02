//go:build linux

package main

import (
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/syscode-labs/imp/internal/agent"
	"github.com/syscode-labs/imp/internal/agent/network"
)

// newProductionDriver creates a FirecrackerDriver wired with a LinuxNetManager.
// Reads FC_BIN, FC_SOCK_DIR, FC_KERNEL, FC_KERNEL_ARGS, and IMP_IMAGE_CACHE.
func newProductionDriver(client ctrlclient.Client) (agent.VMDriver, error) {
	d, err := agent.NewFirecrackerDriver(client)
	if err != nil {
		return nil, err
	}
	d.Net = network.NewLinuxNetManager()
	return d, nil
}
