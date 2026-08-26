package controller

import "testing"

func TestApplyMemoryReserve_subtracts(t *testing.T) {
	got := applyMemoryReserve(8192, 512)
	if got != 7680 {
		t.Errorf("got %d, want 7680", got)
	}
}

func TestApplyMemoryReserve_clampsAtZero(t *testing.T) {
	got := applyMemoryReserve(256, 512)
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestApplyMemoryReserve_zeroReserve(t *testing.T) {
	got := applyMemoryReserve(8192, 0)
	if got != 8192 {
		t.Errorf("got %d, want 8192", got)
	}
}

// TestDefaultMemoryReserveMiB pins the documented default so a silent change
// to scheduling headroom is caught in review.
func TestDefaultMemoryReserveMiB(t *testing.T) {
	if defaultMemoryReserveMiB != 512 {
		t.Errorf("default reserve changed to %d; update openspec design doc if intentional",
			defaultMemoryReserveMiB)
	}
}
