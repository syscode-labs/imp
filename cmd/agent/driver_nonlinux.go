//go:build !linux

package main

import (
	"fmt"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/syscode-labs/imp/internal/agent"
)

// newProductionDriver returns an error on non-Linux platforms because
// Firecracker requires KVM which is only available on Linux.
func newProductionDriver(_ ctrlclient.Client) (agent.VMDriver, error) {
	return nil, fmt.Errorf("FirecrackerDriver requires Linux (KVM not available on this platform)")
}
