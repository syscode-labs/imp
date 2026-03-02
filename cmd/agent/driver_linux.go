//go:build linux

package main

import (
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/syscode-labs/imp/internal/agent"
)

// newProductionDriver creates a FirecrackerDriver from environment variables.
// Reads FC_BIN, FC_SOCK_DIR, FC_KERNEL, FC_KERNEL_ARGS, and IMP_IMAGE_CACHE.
func newProductionDriver(client ctrlclient.Client) (agent.VMDriver, error) {
	return agent.NewFirecrackerDriver(client)
}
