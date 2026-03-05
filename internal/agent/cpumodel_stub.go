//go:build !linux

package agent

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func detectAndPatchCPUModel(_ context.Context, _ client.Client, _ string) {} //nolint:unused
