package telemetry_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/syscode-labs/imp/internal/telemetry"
)

func TestSetupMeterProvider_noOTLP(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	reg := prometheus.NewRegistry()
	mp, shutdown, err := telemetry.SetupMeterProvider(context.Background(), reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mp == nil {
		t.Fatal("mp is nil")
	}
	if shutdown == nil {
		t.Fatal("shutdown is nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}

func TestSetupMeterProvider_withOTLP(t *testing.T) {
	// Point at a non-existent endpoint — exporter connects lazily so no error at setup.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:19999")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	reg := prometheus.NewRegistry()
	mp, shutdown, err := telemetry.SetupMeterProvider(context.Background(), reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mp == nil {
		t.Fatal("mp is nil")
	}
	_ = shutdown(context.Background())
}

func TestSetupMeterProvider_grpcProtocol(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:19999")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")

	reg := prometheus.NewRegistry()
	mp, shutdown, err := telemetry.SetupMeterProvider(context.Background(), reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mp == nil {
		t.Fatal("mp is nil")
	}
	_ = shutdown(context.Background())
}

func TestSetupTracerProvider_noOTLP(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	tp, shutdown, err := telemetry.SetupTracerProvider(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("tp is nil")
	}
	if shutdown == nil {
		t.Fatal("shutdown is nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}

func TestSetupTracerProvider_withOTLPHTTP(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:19999")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	tp, shutdown, err := telemetry.SetupTracerProvider(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("tp is nil")
	}
	_ = shutdown(context.Background())
}

func TestSetupTracerProvider_withOTLPGRPC(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:19999")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")

	tp, shutdown, err := telemetry.SetupTracerProvider(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("tp is nil")
	}
	_ = shutdown(context.Background())
}
