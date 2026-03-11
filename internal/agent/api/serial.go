//go:build linux

package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
)

func (s *APIServer) handleSerial(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	vmName := r.PathValue("vm")
	if len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(vmName)) != 0 {
		http.Error(w, "invalid namespace or vm name", http.StatusBadRequest)
		return
	}

	logPath := filepath.Join(s.SocketDir, namespace+"-"+vmName+".serial.log")

	f, err := os.Open(logPath) //nolint:gosec // G304: path segments validated as Kubernetes DNS-1123 names
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, fmt.Sprintf("serial log for %s/%s not found", namespace, vmName), http.StatusNotFound)
			return
		}
		http.Error(w, "open serial log: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	follow := r.URL.Query().Get("follow") == "true"
	if !follow {
		// Stream the file contents and close.
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f) //nolint:errcheck
		return
	}

	// Tail mode: stream available bytes, then poll every 200ms until the client
	// disconnects (ctx.Done()) or the request context is cancelled.
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	ctx := r.Context()
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n]) //nolint:errcheck
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return
		}
		if err == nil {
			continue // there may be more data immediately
		}
		// EOF — wait for more data or client disconnect.
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
			// poll again
		}
	}
}
