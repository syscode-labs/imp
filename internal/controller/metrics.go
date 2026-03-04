package controller

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// SchedulingLatencyHistogram measures time from ImpVM creation to node assignment.
	SchedulingLatencyHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "imp_vm_scheduling_latency_seconds",
		Help:    "Time from ImpVM creation to node assignment (Pending→Scheduled).",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
	})

	// BootLatencyHistogram measures time from node assignment to VM Running.
	BootLatencyHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "imp_vm_boot_latency_seconds",
		Help:    "Time from ImpVM node assignment to Running state (Scheduled→Running).",
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 8),
	})
)

func init() {
	metrics.Registry.MustRegister(SchedulingLatencyHistogram, BootLatencyHistogram)
}

// ObserveSchedulingLatency records a scheduling latency observation.
func ObserveSchedulingLatency(d time.Duration) {
	SchedulingLatencyHistogram.Observe(d.Seconds())
}

// ObserveBootLatency records a boot latency observation.
func ObserveBootLatency(d time.Duration) {
	BootLatencyHistogram.Observe(d.Seconds())
}

// ResetMetricsForTest is a no-op; histograms are cumulative and cannot be reset.
func ResetMetricsForTest() {}
