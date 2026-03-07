//go:build linux

package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/syscode-labs/imp/internal/agent"
)

func newTestCollector(t *testing.T) (*agent.VMMetricsCollector, http.Handler) {
	t.Helper()
	reg := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(reg))
	if err != nil {
		t.Fatal(err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mc := agent.NewVMMetricsCollector(mp.Meter("test"), reg)
	h := agent.NewMetricsHandlerWithCollector(mc)
	return mc, h
}

func TestVMMetrics_vmStateAppears(t *testing.T) {
	mc, h := newTestCollector(t)
	mc.SetVMState("default/test-vm", "Running", "test-node")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "imp_vm_state") {
		t.Errorf("expected imp_vm_state in output, got:\n%s", w.Body.String())
	}
}

func TestVMMetrics_clearVMRemovesState(t *testing.T) {
	mc, h := newTestCollector(t)
	mc.SetVMState("default/test-vm", "Running", "test-node")
	mc.ClearVM("default/test-vm")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), `impvm="test-vm"`) {
		t.Errorf("expected test-vm to be absent after ClearVM, got:\n%s", w.Body.String())
	}
}

func TestVMMetrics_guestMetricsAppear(t *testing.T) {
	mc, h := newTestCollector(t)
	mc.SetGuestMetrics("default/test-vm", "test-node", "small", 0.5, 0.1, 512*1024*1024, 1024*1024*1024)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	body := w.Body.String()
	for _, metric := range []string{
		"imp_vm_guest_cpu_usage_ratio",
		"imp_vm_guest_cpu_iowait_ratio",
		"imp_vm_guest_memory_used_bytes",
		"imp_vm_guest_disk_used_bytes",
	} {
		if !strings.Contains(body, metric) {
			t.Errorf("expected %s in output, got:\n%s", metric, body)
		}
	}
}
