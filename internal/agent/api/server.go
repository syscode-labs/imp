//go:build linux

// Package api provides the imp plugin HTTP API server that runs on :9091.
// It exposes endpoints for exec (VSOCK RPC) and serial log streaming.
package api

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// VSockDialer is implemented by FirecrackerDriver. It returns the VSOCK Unix
// socket proxy path for a running VM identified by "namespace/name".
type VSockDialer interface {
	GetVSockPath(key string) (string, bool)
}

// APIServer is a controller-runtime Runnable that serves the imp plugin HTTP
// API on :9091.
type APIServer struct {
	// SocketDir is the directory where per-VM serial log files are written.
	SocketDir string
	// Driver provides VSOCK socket paths for running VMs.
	Driver VSockDialer
}

// Start implements sigs.k8s.io/controller-runtime/pkg/manager.Runnable.
func (s *APIServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/exec/{namespace}/{vm}", s.handleExec)
	mux.HandleFunc("GET /v1/serial/{namespace}/{vm}", s.handleSerial)
	srv := &http.Server{Addr: ":9091", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background()) //nolint:errcheck
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
