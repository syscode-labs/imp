package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestCarveGroupCIDRs_TwoGroups(t *testing.T) {
	groups := []impdevv1alpha1.NetworkGroupSpec{
		{Name: "workers", ExpectedSize: 14},    // → /28 (16 addresses)
		{Name: "controllers", ExpectedSize: 4}, // → /29 (8 addresses)
	}
	// controllers < workers alphabetically; controllers gets the first block.
	result, err := carveGroupCIDRs("10.44.0.0/24", groups)
	require.NoError(t, err)
	require.Len(t, result, 2)

	// Map by name for order-independent assertions.
	byName := make(map[string]string, len(result))
	for _, gc := range result {
		byName[gc.Name] = gc.CIDR
	}

	// controllers (sorted first): /29 aligned at 0 → 10.44.0.0/29
	assert.Equal(t, "10.44.0.0/29", byName["controllers"])
	// workers: /28 must start at a 16-address aligned boundary after controllers' block (ends at .8)
	// Next /28-aligned address ≥ 10.44.0.8 is 10.44.0.16.
	assert.Equal(t, "10.44.0.16/28", byName["workers"])
}

func TestCarveGroupCIDRs_SingleGroup(t *testing.T) {
	groups := []impdevv1alpha1.NetworkGroupSpec{
		{Name: "all", ExpectedSize: 30}, // → /27 (32 addresses)
	}
	result, err := carveGroupCIDRs("10.44.0.0/24", groups)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "10.44.0.0/27", result[0].CIDR)
}

func TestCarveGroupCIDRs_NoGroups(t *testing.T) {
	result, err := carveGroupCIDRs("10.44.0.0/24", nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestCarveGroupCIDRs_Overflow(t *testing.T) {
	// /30 parent (4 addresses = 2 usable) cannot fit a /28 (16 addresses).
	groups := []impdevv1alpha1.NetworkGroupSpec{
		{Name: "big", ExpectedSize: 14}, // → /28
	}
	_, err := carveGroupCIDRs("10.44.0.0/30", groups)
	require.Error(t, err)
}

func TestCarveGroupCIDRs_Deterministic(t *testing.T) {
	// Same groups in different order should produce identical output (sorted by name).
	groups1 := []impdevv1alpha1.NetworkGroupSpec{
		{Name: "z", ExpectedSize: 2},
		{Name: "a", ExpectedSize: 2},
	}
	groups2 := []impdevv1alpha1.NetworkGroupSpec{
		{Name: "a", ExpectedSize: 2},
		{Name: "z", ExpectedSize: 2},
	}
	r1, err1 := carveGroupCIDRs("10.0.0.0/24", groups1)
	r2, err2 := carveGroupCIDRs("10.0.0.0/24", groups2)
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, r1[0].CIDR, r2[0].CIDR) // "a" gets same CIDR regardless of input order
}
