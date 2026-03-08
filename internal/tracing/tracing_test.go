package tracing_test

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/tracing"
)

// setupTracer installs a real SDK tracer provider with AlwaysSample as the global OTel provider.
// It mutates global OTel state, so tests that use it must not call t.Parallel().
func setupTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { otel.SetTracerProvider(sdktrace.NewTracerProvider()) })
	return sr
}

func TestSpanFromVM_noAnnotation_startsRootSpan(t *testing.T) {
	sr := setupTracer(t)
	vm := &impdevv1alpha1.ImpVM{ObjectMeta: metav1.ObjectMeta{Name: "test-vm", Namespace: "default"}}

	ctx, span := tracing.SpanFromVM(context.Background(), vm, "test.span")
	span.End()
	_ = ctx

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "test.span" {
		t.Errorf("expected span name %q, got %q", "test.span", spans[0].Name())
	}
}

func TestSpanFromVM_withAnnotation_isChildSpan(t *testing.T) {
	sr := setupTracer(t)

	// Create a parent span and inject its context into VM annotations
	parentCtx, parentSpan := otel.Tracer("test").Start(context.Background(), "parent")
	vm := &impdevv1alpha1.ImpVM{ObjectMeta: metav1.ObjectMeta{Name: "test-vm", Namespace: "default"}}
	tracing.InjectToVM(parentCtx, vm, parentSpan)
	parentSpan.End()

	_, child := tracing.SpanFromVM(context.Background(), vm, "child.span")
	child.End()

	spans := sr.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	parentTraceID := spans[0].SpanContext().TraceID()
	childTraceID := spans[1].SpanContext().TraceID()
	if parentTraceID != childTraceID {
		t.Errorf("trace IDs differ: parent=%s child=%s", parentTraceID, childTraceID)
	}
}

func TestInjectToVM_nilAnnotations_initializesMap(t *testing.T) {
	setupTracer(t)
	vm := &impdevv1alpha1.ImpVM{ObjectMeta: metav1.ObjectMeta{Name: "test-vm", Namespace: "default"}}
	// vm.Annotations is nil

	_, span := otel.Tracer("test").Start(context.Background(), "parent")
	tracing.InjectToVM(context.Background(), vm, span)
	span.End()

	if vm.Annotations == nil {
		t.Error("expected Annotations to be initialized")
	}
	if vm.Annotations["imp.dev/trace-context"] == "" {
		t.Error("expected imp.dev/trace-context annotation to be non-empty")
	}
}

func TestRecordError_nil_noops(t *testing.T) {
	sr := setupTracer(t)
	_, span := otel.Tracer("test").Start(context.Background(), "s")
	tracing.RecordError(span, nil)
	span.End()

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("expected 1 span")
	}
	if spans[0].Status().Code != codes.Unset {
		t.Errorf("expected Unset status, got %v", spans[0].Status().Code)
	}
}

func TestRecordError_nonNil_setsErrorStatus(t *testing.T) {
	sr := setupTracer(t)
	_, span := otel.Tracer("test").Start(context.Background(), "s")
	tracing.RecordError(span, fmt.Errorf("boom"))
	span.End()

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("expected 1 span")
	}
	if spans[0].Status().Code != codes.Error {
		t.Errorf("expected Error status, got %v", spans[0].Status().Code)
	}
}
