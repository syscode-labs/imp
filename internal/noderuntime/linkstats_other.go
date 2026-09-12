//go:build !linux

package noderuntime

import (
	"context"
	"errors"
)

func runtimeLinkStats(_ context.Context, _ string) (uint64, error) {
	return 0, errors.New("runtime link stats is not supported on this platform")
}
