package cnidetect_test

import (
	"testing"

	"github.com/syscode-labs/imp/internal/cnidetect"
)

func TestStore(t *testing.T) {
	s := &cnidetect.Store{}

	// Unset returns false.
	if _, ok := s.Result(); ok {
		t.Fatal("expected no result before Set")
	}

	want := cnidetect.Result{Provider: cnidetect.ProviderCilium, NATBackend: cnidetect.NATBackendNftables}
	s.Set(want)

	got, ok := s.Result()
	if !ok {
		t.Fatal("expected result after Set")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
