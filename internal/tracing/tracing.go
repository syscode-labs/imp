// Package tracing provides shared OpenTelemetry span helpers for imp binaries.
package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// SpanFromVM extracts the W3C trace context from vm's annotations (keys
// "imp.dev/trace-context" and "imp.dev/trace-state") and starts a child span
// with the given name. When annotations are absent or invalid, starts a root span.
func SpanFromVM(ctx context.Context, vm *impdevv1alpha1.ImpVM, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	carrier := propagation.MapCarrier{
		"traceparent": vm.Annotations["imp.dev/trace-context"],
		"tracestate":  vm.Annotations["imp.dev/trace-state"],
	}
	remoteCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)
	return otel.Tracer("imp.agent").Start(remoteCtx, spanName, opts...)
}

// InjectToVM injects the current trace context from ctx into vm's annotations.
// Used by the operator when scheduling a VM so the agent can continue the trace.
// span is the current active span whose context should be propagated.
func InjectToVM(ctx context.Context, vm *impdevv1alpha1.ImpVM, span trace.Span) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(trace.ContextWithSpan(ctx, span), carrier)
	if carrier["traceparent"] == "" {
		return
	}
	if vm.Annotations == nil {
		vm.Annotations = map[string]string{}
	}
	vm.Annotations["imp.dev/trace-context"] = carrier["traceparent"]
	if ts := carrier["tracestate"]; ts != "" {
		vm.Annotations["imp.dev/trace-state"] = ts
	}
}

// RecordError marks span as errored and records err as a span event.
// No-op when err is nil.
func RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
