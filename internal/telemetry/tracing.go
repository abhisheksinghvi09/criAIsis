package telemetry

import (
	"context"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracer is the service's tracer. Until InitTracing installs a provider it is a
// no-op, so instrumented code costs nothing when tracing is off.
func Tracer() trace.Tracer { return otel.Tracer("criaisis") }

// InitTracing exports spans over OTLP/HTTP when the standard OpenTelemetry
// environment names a collector (OTEL_EXPORTER_OTLP_ENDPOINT or
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT). Sampling follows OTEL_TRACES_SAMPLER /
// OTEL_TRACES_SAMPLER_ARG, defaulting to parent-based always-on. With no endpoint
// it installs nothing and returns a no-op shutdown.
func InitTracing(ctx context.Context, version string) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return noop, nil
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceName("criaisis"), semconv.ServiceVersion(version)))
	if err != nil {
		return noop, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter), // async: exporting never blocks a request
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider.Shutdown, nil
}

// HTTPHandler wraps the router so every request gets a server span, continuing
// any trace the caller propagated. Middleware renames the span to the route.
func HTTPHandler(h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, "http.request")
}
