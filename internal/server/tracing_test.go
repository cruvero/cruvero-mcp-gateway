package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func TestInitTracerCreatesProviderAndShutdown(t *testing.T) {
	SetTracingEndpoint("")
	SetTracingVersion("test-version")

	shutdown, err := InitTracer(context.Background(), "mcpgw-test")
	if err != nil {
		t.Fatalf("init tracer: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function")
	}
	shutdown()
}

func TestInitTracerWithDefaultServiceNameAndEndpoint(t *testing.T) {
	SetTracingEndpoint("http://127.0.0.1:4318")
	SetTracingVersion("v-test")

	shutdown, err := InitTracer(context.Background(), "")
	if err != nil {
		t.Fatalf("init tracer with defaults: %v", err)
	}
	shutdown()
}

func TestTracingMiddlewareCreatesSpanWithAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("mcpgw-test"),
		)),
	)
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()
	otel.SetTracerProvider(tp)

	handler := TracingMiddleware(tp.Tracer("test"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SpanFromContext(r.Context()).SetAttributes(attribute.String("test.attr", "ok"))
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/trace-test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected spans to be exported")
	}

	span := spans[len(spans)-1]
	if span.Name != "gateway.request" {
		t.Fatalf("expected span name gateway.request, got %q", span.Name)
	}

	attrs := make(map[string]any)
	for _, kv := range span.Attributes {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}
	if attrs["http.method"] != http.MethodPost {
		t.Fatalf("expected http.method POST, got %#v", attrs["http.method"])
	}
	if attrs["http.path"] != "/trace-test" {
		t.Fatalf("expected http.path /trace-test, got %#v", attrs["http.path"])
	}
	if attrs["http.status_code"] != int64(http.StatusAccepted) {
		t.Fatalf("expected http.status_code 202, got %#v", attrs["http.status_code"])
	}
}

func TestSpanFromContextReturnsActiveSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("span-test").Start(context.Background(), "root")
	got := SpanFromContext(ctx)
	if got == nil {
		t.Fatal("expected active span")
	}
	span.End()
}

func TestTracingSetters(t *testing.T) {
	SetTracingVersion("")
	if got := currentTracingVersion(); got != "dev" {
		t.Fatalf("expected tracing version fallback dev, got %q", got)
	}

	SetTracingVersion("1.2.3")
	if got := currentTracingVersion(); got != "1.2.3" {
		t.Fatalf("expected tracing version 1.2.3, got %q", got)
	}

	SetTracingEndpoint(" http://collector:4318 ")
	if got := currentTracingEndpoint(); got != "http://collector:4318" {
		t.Fatalf("expected trimmed endpoint, got %q", got)
	}
}

func TestTracingMiddlewareWithNilTracer(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()
	otel.SetTracerProvider(tp)

	handler := TracingMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/nil-tracer", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if len(exporter.GetSpans()) == 0 {
		t.Fatal("expected span with nil tracer fallback")
	}
}
