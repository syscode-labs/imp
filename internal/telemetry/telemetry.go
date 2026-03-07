// Package telemetry provides shared OpenTelemetry MeterProvider setup for imp binaries.
package telemetry

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// SetupMeterProvider creates an OTel MeterProvider.
//
// A Prometheus pull exporter is always registered into reg.
// If OTEL_EXPORTER_OTLP_ENDPOINT is set, an OTLP push exporter is also added.
// Protocol is selected by OTEL_EXPORTER_OTLP_PROTOCOL ("grpc" or "http/protobuf",
// default "http/protobuf"). Export interval defaults to 30s; override with
// OTEL_METRIC_EXPORT_INTERVAL (milliseconds).
//
// The returned shutdown function must be called on process exit.
func SetupMeterProvider(ctx context.Context, reg prometheus.Registerer) (*sdkmetric.MeterProvider, func(context.Context) error, error) {
	promExporter, err := otelprometheus.New(otelprometheus.WithRegisterer(reg))
	if err != nil {
		return nil, nil, fmt.Errorf("prometheus exporter: %w", err)
	}

	opts := []sdkmetric.Option{sdkmetric.WithReader(promExporter)}

	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		interval := 30 * time.Second
		if s := os.Getenv("OTEL_METRIC_EXPORT_INTERVAL"); s != "" {
			if ms, parseErr := strconv.Atoi(s); parseErr == nil {
				interval = time.Duration(ms) * time.Millisecond
			}
		}

		var otlpExporter sdkmetric.Exporter
		switch os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL") {
		case "grpc":
			otlpExporter, err = otlpmetricgrpc.New(ctx)
		default: // "http/protobuf" or empty
			otlpExporter, err = otlpmetrichttp.New(ctx)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("otlp exporter: %w", err)
		}

		opts = append(opts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(otlpExporter, sdkmetric.WithInterval(interval)),
		))
	}

	mp := sdkmetric.NewMeterProvider(opts...)
	return mp, mp.Shutdown, nil
}
