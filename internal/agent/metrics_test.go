//go:build linux

package agent_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syscode-labs/imp/internal/agent"
)

func TestMetricsHandler_serves(t *testing.T) {
	h := agent.NewMetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "go_") {
		t.Error("expected Prometheus default Go metrics in response")
	}
}

func TestVMMetrics_registered(t *testing.T) {
	collector := agent.NewVMMetricsCollector()
	collector.SetVMState("default/test-vm", "Running", "test-node")
	h := agent.NewMetricsHandlerWithCollector(collector)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "imp_vm_state") {
		t.Errorf("expected imp_vm_state in metrics output, got:\n%s", w.Body.String())
	}
}
