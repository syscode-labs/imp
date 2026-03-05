package network

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSizeToCIDRPrefix(t *testing.T) {
	tests := []struct {
		n    int32
		want int
	}{
		{0, 30},   // zero → /30
		{1, 30},   // 1 host → /30 (2 usable)
		{2, 30},   // 2 hosts → /30 (exactly fits)
		{3, 29},   // 3 hosts → /29 (6 usable, next up from /30)
		{14, 28},  // 14 hosts → /28 (14 usable)
		{15, 27},  // 15 hosts needs /27 (30 usable)
		{62, 26},  // 62 hosts → /26 (62 usable)
		{63, 25},  // 63 hosts needs /25 (126 usable)
		{254, 24}, // 254 hosts → /24 (254 usable)
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			got := sizeToCIDRPrefix(tt.n)
			assert.Equal(t, tt.want, got)
		})
	}
}
