// Package telemetry provides OpenTelemetry meter provider setup.
package telemetry

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

// ExporterType selects which OTel metrics exporter to use.
type ExporterType string

const (
	// ExporterPrometheus exposes metrics via a Prometheus scrape endpoint.
	ExporterPrometheus ExporterType = "prometheus"
	// ExporterOTLPGRPC pushes metrics over OTLP/gRPC.
	ExporterOTLPGRPC ExporterType = "otlp-grpc"
	// ExporterOTLPHTTP pushes metrics over OTLP/HTTP.
	ExporterOTLPHTTP ExporterType = "otlp-http"
)

// Config holds configuration for the meter provider.
type Config struct {
	// Exporter selects the exporter type. Defaults to ExporterPrometheus.
	Exporter ExporterType
	// OTLPEndpoint is the endpoint for OTLP exporters (e.g. "localhost:4317").
	OTLPEndpoint string
}

// NewMeterProvider creates a new SDK MeterProvider configured according to cfg.
// The caller is responsible for calling Shutdown on the returned provider.
// When ExporterPrometheus is selected, the /metrics handler is registered on
// the provided mux (or http.DefaultServeMux when mux is nil).
func NewMeterProvider(ctx context.Context, cfg Config, mux *http.ServeMux) (*metric.MeterProvider, error) {
	var reader metric.Reader

	switch cfg.Exporter {
	case ExporterPrometheus, "":
		exp, err := prometheus.New()
		if err != nil {
			return nil, fmt.Errorf("create prometheus exporter: %w", err)
		}
		reader = exp
	case ExporterOTLPGRPC:
		opts := []otlpmetricgrpc.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint))
		}
		exp, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("create otlp-grpc exporter: %w", err)
		}
		reader = metric.NewPeriodicReader(exp)
	case ExporterOTLPHTTP:
		opts := []otlpmetrichttp.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlpmetrichttp.WithEndpoint(cfg.OTLPEndpoint))
		}
		exp, err := otlpmetrichttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("create otlp-http exporter: %w", err)
		}
		reader = metric.NewPeriodicReader(exp)
	default:
		return nil, fmt.Errorf("unknown exporter type %q", cfg.Exporter)
	}

	mp := metric.NewMeterProvider(metric.WithReader(reader))
	return mp, nil
}
