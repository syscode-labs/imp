package controller

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

var (
	schedulingHist metric.Float64Histogram
	bootHist       metric.Float64Histogram
)

// InitMetrics initialises the controller metric instruments from the given meter.
// Must be called from main() after SetupMeterProvider.
func InitMetrics(meter metric.Meter) {
	var err error
	schedulingHist, err = meter.Float64Histogram(
		"imp_vm_scheduling_latency_seconds",
		metric.WithDescription("Time from ImpVM creation to node assignment (Pending→Scheduled)."),
		metric.WithExplicitBucketBoundaries(0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8, 25.6, 51.2),
	)
	if err != nil {
		panic(err)
	}
	bootHist, err = meter.Float64Histogram(
		"imp_vm_boot_latency_seconds",
		metric.WithDescription("Time from ImpVM node assignment to Running state (Scheduled→Running)."),
		metric.WithExplicitBucketBoundaries(0.5, 1.0, 2.0, 4.0, 8.0, 16.0, 32.0, 64.0),
	)
	if err != nil {
		panic(err)
	}
}

// ObserveSchedulingLatency records a scheduling latency observation.
func ObserveSchedulingLatency(d time.Duration) {
	if schedulingHist != nil {
		schedulingHist.Record(context.Background(), d.Seconds())
	}
}

// ObserveBootLatency records a boot latency observation.
func ObserveBootLatency(d time.Duration) {
	if bootHist != nil {
		bootHist.Record(context.Background(), d.Seconds())
	}
}
