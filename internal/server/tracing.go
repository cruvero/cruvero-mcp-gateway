package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracingMu       sync.RWMutex
	tracingVersion  = "dev"
	tracingEndpoint string
)

// InitTracer initializes OTLP tracing and returns a shutdown function.
func InitTracer(ctx context.Context, serviceName string) (func(), error) {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		name = "mcpgw"
	}
	endpoint := currentTracingEndpoint()

	opts := []otlptracehttp.Option{}
	if endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpointURL(endpoint))
	}
	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("init tracer: create otlp exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(name),
			semconv.ServiceVersion(currentTracingVersion()),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("init tracer: create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func() {
		_ = tp.Shutdown(context.Background())
	}
	return shutdown, nil
}

// SetTracingVersion sets service.version resource attribute used by InitTracer.
func SetTracingVersion(version string) {
	tracingMu.Lock()
	defer tracingMu.Unlock()
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		tracingVersion = "dev"
		return
	}
	tracingVersion = trimmed
}

// SetTracingEndpoint sets OTLP endpoint URL used by InitTracer.
func SetTracingEndpoint(endpoint string) {
	tracingMu.Lock()
	defer tracingMu.Unlock()
	tracingEndpoint = strings.TrimSpace(endpoint)
}

// TracingMiddleware creates a root span for each HTTP request.
func TracingMiddleware(tracer trace.Tracer) func(http.Handler) http.Handler {
	if tracer == nil {
		tracer = otel.Tracer("mcpgw/server")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			ctx, span := tracer.Start(r.Context(), "gateway.request",
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.String()),
					attribute.String("http.path", r.URL.Path),
				),
			)
			defer span.End()

			next.ServeHTTP(recorder, r.WithContext(ctx))
			span.SetAttributes(attribute.Int("http.status_code", recorder.statusCode))
		})
	}
}

// SpanFromContext returns the active span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

func currentTracingVersion() string {
	tracingMu.RLock()
	defer tracingMu.RUnlock()
	return tracingVersion
}

func currentTracingEndpoint() string {
	tracingMu.RLock()
	defer tracingMu.RUnlock()
	return tracingEndpoint
}
