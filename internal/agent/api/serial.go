//go:build linux

package api

import "net/http"

func (s *APIServer) handleSerial(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
