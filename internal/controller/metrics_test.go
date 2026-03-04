package controller

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// sampleCount reads the cumulative sample count from a histogram.
func sampleCount(h prometheus.Histogram) uint64 {
	var m dto.Metric
	_ = h.Write(&m) //nolint:errcheck
	return m.GetHistogram().GetSampleCount()
}

func TestSchedulingLatency_observed(t *testing.T) {
	before := sampleCount(SchedulingLatencyHistogram)
	ObserveSchedulingLatency(500 * time.Millisecond)
	after := sampleCount(SchedulingLatencyHistogram)
	if after != before+1 {
		t.Errorf("sample count: got %d, want %d", after, before+1)
	}
}

func TestBootLatency_observed(t *testing.T) {
	before := sampleCount(BootLatencyHistogram)
	ObserveBootLatency(2 * time.Second)
	after := sampleCount(BootLatencyHistogram)
	if after != before+1 {
		t.Errorf("sample count: got %d, want %d", after, before+1)
	}
}
