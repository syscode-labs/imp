//go:build linux

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	agentvsock "github.com/syscode-labs/imp/internal/agent/vsock"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
)

// execRequest is the JSON body for POST /v1/exec/{namespace}/{vm}.
type execRequest struct {
	Command []string `json:"command"`
}

// execLine is one line of the streaming NDJSON response.
type execLine struct {
	Stream string `json:"stream"`         // "stdout", "stderr", or "exit"
	Line   string `json:"line,omitempty"` // present for stdout/stderr
	Code   *int32 `json:"code,omitempty"` // present for exit
}

func (s *APIServer) handleExec(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	vmName := r.PathValue("vm")
	key := namespace + "/" + vmName

	vsockPath, ok := s.Driver.GetVSockPath(key)
	if !ok {
		http.Error(w, fmt.Sprintf("VM %s not found or guest agent not enabled", key), http.StatusNotFound)
		return
	}

	var req execRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Command) == 0 {
		http.Error(w, "command must not be empty", http.StatusBadRequest)
		return
	}

	conn, err := agentvsock.Dial(r.Context(), vsockPath, 10000)
	if err != nil {
		http.Error(w, "dial vsock: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer conn.Close() //nolint:errcheck

	client := pb.NewGuestAgentClient(conn)
	resp, err := client.Exec(r.Context(), &pb.ExecRequest{Command: req.Command})
	if err != nil {
		http.Error(w, "exec RPC: "+err.Error(), http.StatusBadGateway)
		return
	}

	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	// Emit stdout lines.
	for _, line := range strings.Split(resp.Stdout, "\n") {
		if line == "" {
			continue
		}
		_ = enc.Encode(execLine{Stream: "stdout", Line: line}) //nolint:errcheck
		flush()
	}

	// Emit stderr lines.
	for _, line := range strings.Split(resp.Stderr, "\n") {
		if line == "" {
			continue
		}
		_ = enc.Encode(execLine{Stream: "stderr", Line: line}) //nolint:errcheck
		flush()
	}

	// Emit exit code.
	code := resp.ExitCode
	_ = enc.Encode(execLine{Stream: "exit", Code: &code}) //nolint:errcheck
	flush()
}
