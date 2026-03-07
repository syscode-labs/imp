package controller

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func histogramCount(rm metricdata.ResourceMetrics, name string) uint64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if hist, ok := m.Data.(metricdata.Histogram[float64]); ok {
					var total uint64
					for _, dp := range hist.DataPoints {
						total += dp.Count
					}
					return total
				}
			}
		}
	}
	return 0
}

func setupTestMetrics(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
		schedulingHist = nil
		bootHist = nil
	})
	InitMetrics(mp.Meter("test"))
	return reader
}

func TestSchedulingLatency_observed(t *testing.T) {
	reader := setupTestMetrics(t)

	ObserveSchedulingLatency(500 * time.Millisecond)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	if got := histogramCount(rm, "imp_vm_scheduling_latency_seconds"); got != 1 {
		t.Errorf("sample count = %d, want 1", got)
	}
}

func TestBootLatency_observed(t *testing.T) {
	reader := setupTestMetrics(t)

	ObserveBootLatency(2 * time.Second)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	if got := histogramCount(rm, "imp_vm_boot_latency_seconds"); got != 1 {
		t.Errorf("sample count = %d, want 1", got)
	}
}

func TestObserve_noopWhenUninitialized(t *testing.T) {
	schedulingHist = nil
	bootHist = nil
	ObserveSchedulingLatency(100 * time.Millisecond)
	ObserveBootLatency(200 * time.Millisecond)
}
