//go:build !linux

package noderuntime

import (
	"context"
	"errors"
)

// WakeHits is unsupported off Linux; the agent falls back to no-op polling.
func (b *Backend) WakeHits(_ context.Context) ([]string, error) {
	_ = b.wakeHitsState // linux-only capture state; unused here
	return nil, errors.New("runtime wake hits is not supported on this platform")
}
