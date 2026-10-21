package telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"criaisis/internal/telemetry"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestMiddleware_LogsRouteNotQueryAndNeverLeaksIt(t *testing.T) {
	var out strings.Builder
	log := zerolog.New(&out)
	r := chi.NewRouter()
	r.Use(telemetry.Middleware(&log))
	r.Get("/things/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/things/42?code=oauth-secret", nil))

	line := out.String()
	if !strings.Contains(line, `"route":"/things/{id}"`) || !strings.Contains(line, `"status":418`) {
		t.Errorf("unexpected log line: %s", line)
	}
	if strings.Contains(line, "oauth-secret") {
		t.Errorf("query string leaked into the log: %s", line)
	}
}

func TestHandler_RequiresTheBearerToken(t *testing.T) {
	h := telemetry.Handler("s3cret")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "criaisis_") && !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Errorf("with token: status = %d", rec.Code)
	}
}

func TestTracing_NamesSpansByRouteAndCorrelatesLogs(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	var out strings.Builder
	log := zerolog.New(&out)
	r := chi.NewRouter()
	r.Use(telemetry.Middleware(&log))
	r.Get("/incidents/{id}", func(w http.ResponseWriter, r *http.Request) {})

	telemetry.HTTPHandler(r).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/incidents/123", nil))

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "GET /incidents/{id}" {
		t.Fatalf("spans = %v, want one named by route", spans)
	}
	if want := spans[0].SpanContext().TraceID().String(); !strings.Contains(out.String(), want) {
		t.Errorf("access log lacks trace id %s: %s", want, out.String())
	}
}

func TestInitTracing_IsANoOpWithoutAnEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	shutdown, err := telemetry.InitTracing(context.Background(), "test")
	if err != nil || shutdown(context.Background()) != nil {
		t.Errorf("no-op tracing returned an error: %v", err)
	}
}
