package telemetry_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"criaisis/internal/telemetry"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
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
