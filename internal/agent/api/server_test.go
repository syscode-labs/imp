//go:build linux

package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// mockDriver is a test VSockDialer.
type mockDriver struct {
	path string
	ok   bool
}

func (m *mockDriver) GetVSockPath(_ string) (string, bool) {
	return m.path, m.ok
}

// execLineJSON is used in tests to decode exec stream lines.
type execLineJSON struct {
	Stream string `json:"stream"`
	Line   string `json:"line"`
	Code   *int32 `json:"code"`
}

// parseNDJSON reads all NDJSON lines from b.
func parseNDJSON(t *testing.T, b *bytes.Buffer) []execLineJSON {
	t.Helper()
	var lines []execLineJSON
	scanner := bufio.NewScanner(b)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		var l execLineJSON
		if err := json.Unmarshal([]byte(text), &l); err != nil {
			t.Fatalf("parse ndjson line %q: %v", text, err)
		}
		lines = append(lines, l)
	}
	return lines
}

// TestHandleExec_NotFound verifies a 404 when the VM is not in the driver.
func TestHandleExec_NotFound(t *testing.T) {
	srv := &APIServer{
		SocketDir: t.TempDir(),
		Driver:    &mockDriver{ok: false},
	}
	body := `{"command":["echo","hello"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/exec/default/myvm", strings.NewReader(body))
	req.SetPathValue("namespace", "default")
	req.SetPathValue("vm", "myvm")
	rec := httptest.NewRecorder()
	srv.handleExec(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// TestHandleExec_BadBody verifies a 400 on invalid JSON body.
func TestHandleExec_BadBody(t *testing.T) {
	srv := &APIServer{
		SocketDir: t.TempDir(),
		Driver:    &mockDriver{ok: true, path: "/nonexistent.vsock"},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/exec/default/myvm", strings.NewReader("not-json"))
	req.SetPathValue("namespace", "default")
	req.SetPathValue("vm", "myvm")
	rec := httptest.NewRecorder()
	srv.handleExec(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestHandleExec_VSockUnavailable verifies a 502 when the VSOCK dial fails.
func TestHandleExec_VSockUnavailable(t *testing.T) {
	srv := &APIServer{
		SocketDir: t.TempDir(),
		Driver:    &mockDriver{ok: true, path: "/tmp/nonexistent-imp-test.vsock"},
	}
	body := `{"command":["echo","hello"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/exec/default/myvm", strings.NewReader(body))
	req.SetPathValue("namespace", "default")
	req.SetPathValue("vm", "myvm")
	rec := httptest.NewRecorder()
	srv.handleExec(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// TestHandleSerial_NotFound verifies 404 when the serial log file does not exist.
func TestHandleSerial_NotFound(t *testing.T) {
	srv := &APIServer{
		SocketDir: t.TempDir(),
		Driver:    &mockDriver{ok: false},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/serial/default/myvm", nil)
	req.SetPathValue("namespace", "default")
	req.SetPathValue("vm", "myvm")
	rec := httptest.NewRecorder()
	srv.handleSerial(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// TestHandleSerial_ReadFile verifies that existing serial log content is returned.
func TestHandleSerial_ReadFile(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/default-myvm.serial.log"
	content := "boot line 1\nboot line 2\n"
	if err := os.WriteFile(logPath, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	srv := &APIServer{
		SocketDir: dir,
		Driver:    &mockDriver{ok: false},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/serial/default/myvm", nil)
	req.SetPathValue("namespace", "default")
	req.SetPathValue("vm", "myvm")
	rec := httptest.NewRecorder()
	srv.handleSerial(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != content {
		t.Fatalf("expected body %q, got %q", content, got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected Content-Type: %q", ct)
	}
}
