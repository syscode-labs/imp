package controller

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSchedulingLatency_observed(t *testing.T) {
	ObserveSchedulingLatency(500 * time.Millisecond)

	// Use testutil.CollectAndCount to verify the histogram has observations
	// The histogram name is "imp_vm_scheduling_latency_seconds"
	count := testutil.CollectAndCount(SchedulingLatencyHistogram)
	if count == 0 {
		t.Error("expected scheduling latency histogram to have observations")
	}
}

func TestBootLatency_observed(t *testing.T) {
	ObserveBootLatency(2 * time.Second)

	count := testutil.CollectAndCount(BootLatencyHistogram)
	if count == 0 {
		t.Error("expected boot latency histogram to have observations")
	}
}
