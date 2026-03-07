// Package telemetry provides shared OpenTelemetry MeterProvider setup for imp binaries.
package telemetry

import (
	_ "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	_ "go.opentelemetry.io/otel/exporters/prometheus"
	_ "go.opentelemetry.io/otel/sdk/metric"
)
