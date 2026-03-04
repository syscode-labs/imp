//go:build linux

package agent

import (
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const metricsPort = ":9090"

// VMMetricsCollector holds per-VM metric state for the node agent.
type VMMetricsCollector struct {
	vmState   *prometheus.GaugeVec
	guestCPU  *prometheus.GaugeVec
	guestMem  *prometheus.GaugeVec
	guestDisk *prometheus.GaugeVec
	reg       *prometheus.Registry
}

// NewVMMetricsCollector creates a new collector with its own registry.
func NewVMMetricsCollector() *VMMetricsCollector {
	reg := prometheus.NewRegistry()
	c := &VMMetricsCollector{
		vmState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imp_vm_state",
			Help: "Current VM state (1 = active state).",
		}, []string{"impvm", "namespace", "node", "state"}),
		guestCPU: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imp_vm_guest_cpu_usage_ratio",
			Help: "Guest VM CPU usage ratio (0.0–1.0).",
		}, []string{"impvm", "namespace", "node", "impvmclass"}),
		guestMem: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imp_vm_guest_memory_used_bytes",
			Help: "Guest VM memory used bytes.",
		}, []string{"impvm", "namespace", "node", "impvmclass"}),
		guestDisk: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imp_vm_guest_disk_used_bytes",
			Help: "Guest VM root disk used bytes.",
		}, []string{"impvm", "namespace", "node", "impvmclass"}),
		reg: reg,
	}
	reg.MustRegister(c.vmState, c.guestCPU, c.guestMem, c.guestDisk)
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return c
}

// SetVMState sets the imp_vm_state gauge for a VM. key = "namespace/name".
func (c *VMMetricsCollector) SetVMState(key, state, node string) {
	ns, name := splitKey(key)
	c.vmState.WithLabelValues(name, ns, node, state).Set(1)
}

// SetGuestMetrics updates guest agent metrics for a VM.
func (c *VMMetricsCollector) SetGuestMetrics(key, node, impvmclass string, cpu float64, mem, disk int64) {
	ns, name := splitKey(key)
	c.guestCPU.WithLabelValues(name, ns, node, impvmclass).Set(cpu)
	c.guestMem.WithLabelValues(name, ns, node, impvmclass).Set(float64(mem))
	c.guestDisk.WithLabelValues(name, ns, node, impvmclass).Set(float64(disk))
}

// ClearVM removes all metric series for a VM when it's deleted.
func (c *VMMetricsCollector) ClearVM(key string) {
	ns, name := splitKey(key)
	c.vmState.DeletePartialMatch(prometheus.Labels{"impvm": name, "namespace": ns})
	c.guestCPU.DeletePartialMatch(prometheus.Labels{"impvm": name, "namespace": ns})
	c.guestMem.DeletePartialMatch(prometheus.Labels{"impvm": name, "namespace": ns})
	c.guestDisk.DeletePartialMatch(prometheus.Labels{"impvm": name, "namespace": ns})
}

// NewMetricsHandler returns an HTTP handler for the default Prometheus registry.
func NewMetricsHandler() http.Handler {
	return promhttp.Handler()
}

// NewMetricsHandlerWithCollector returns an HTTP handler for the given collector's registry.
func NewMetricsHandlerWithCollector(c *VMMetricsCollector) http.Handler {
	return promhttp.HandlerFor(c.reg, promhttp.HandlerOpts{})
}

func splitKey(key string) (ns, name string) {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}
