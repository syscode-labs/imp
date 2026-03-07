//go:build linux

package agent

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const metricsPort = ":9090"

type vmStateEntry struct {
	state string
	node  string
}

type guestData struct {
	cpu   float64
	mem   float64
	disk  float64
	node  string
	class string
}

// VMMetricsCollector holds per-VM metric state for the node agent.
type VMMetricsCollector struct {
	mu           sync.RWMutex
	vmStates     map[string]vmStateEntry // "ns/name" → {state, node}
	guestMetrics map[string]*guestData   // "ns/name" → data
	gatherer     prometheus.Gatherer
}

// NewVMMetricsCollector creates a new collector using the provided OTel meter.
// gatherer is the Prometheus registry used by the OTel Prometheus exporter;
// it is used to serve the /metrics HTTP handler.
func NewVMMetricsCollector(meter metric.Meter, gatherer prometheus.Gatherer) *VMMetricsCollector {
	c := &VMMetricsCollector{
		vmStates:     make(map[string]vmStateEntry),
		guestMetrics: make(map[string]*guestData),
		gatherer:     gatherer,
	}

	_, _ = meter.Float64ObservableGauge(
		"imp_vm_state",
		metric.WithDescription("Current VM state (1 = active state)."),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			c.mu.RLock()
			defer c.mu.RUnlock()
			for key, entry := range c.vmStates {
				ns, name := splitKey(key)
				o.Observe(1, metric.WithAttributes(
					attribute.String("impvm", name),
					attribute.String("namespace", ns),
					attribute.String("node", entry.node),
					attribute.String("state", entry.state),
				))
			}
			return nil
		}),
	)

	_, _ = meter.Float64ObservableGauge(
		"imp_vm_guest_cpu_usage_ratio",
		metric.WithDescription("Guest VM CPU usage ratio (0.0–1.0)."),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			c.mu.RLock()
			defer c.mu.RUnlock()
			for key, d := range c.guestMetrics {
				ns, name := splitKey(key)
				o.Observe(d.cpu, metric.WithAttributes(
					attribute.String("impvm", name),
					attribute.String("namespace", ns),
					attribute.String("node", d.node),
					attribute.String("impvmclass", d.class),
				))
			}
			return nil
		}),
	)

	_, _ = meter.Float64ObservableGauge(
		"imp_vm_guest_memory_used_bytes",
		metric.WithDescription("Guest VM memory used bytes."),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			c.mu.RLock()
			defer c.mu.RUnlock()
			for key, d := range c.guestMetrics {
				ns, name := splitKey(key)
				o.Observe(d.mem, metric.WithAttributes(
					attribute.String("impvm", name),
					attribute.String("namespace", ns),
					attribute.String("node", d.node),
					attribute.String("impvmclass", d.class),
				))
			}
			return nil
		}),
	)

	_, _ = meter.Float64ObservableGauge(
		"imp_vm_guest_disk_used_bytes",
		metric.WithDescription("Guest VM root disk used bytes."),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			c.mu.RLock()
			defer c.mu.RUnlock()
			for key, d := range c.guestMetrics {
				ns, name := splitKey(key)
				o.Observe(d.disk, metric.WithAttributes(
					attribute.String("impvm", name),
					attribute.String("namespace", ns),
					attribute.String("node", d.node),
					attribute.String("impvmclass", d.class),
				))
			}
			return nil
		}),
	)

	return c
}

// SetVMState sets the current state for a VM. Only one state is active per VM at a time.
func (c *VMMetricsCollector) SetVMState(key, state, node string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vmStates[key] = vmStateEntry{state: state, node: node}
}

// SetGuestMetrics updates guest agent metrics for a VM.
func (c *VMMetricsCollector) SetGuestMetrics(key, node, impvmclass string, cpu float64, mem, disk int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.guestMetrics[key] = &guestData{
		cpu:   cpu,
		mem:   float64(mem),
		disk:  float64(disk),
		node:  node,
		class: impvmclass,
	}
}

// ClearVM removes all metric state for a VM when it is deleted.
func (c *VMMetricsCollector) ClearVM(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.vmStates, key)
	delete(c.guestMetrics, key)
}

// NewMetricsHandlerWithCollector returns an HTTP handler for the collector's Prometheus registry.
func NewMetricsHandlerWithCollector(c *VMMetricsCollector) http.Handler {
	return promhttp.HandlerFor(c.gatherer, promhttp.HandlerOpts{})
}

func splitKey(key string) (ns, name string) {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}
