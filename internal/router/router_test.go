package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"criaisis/internal/config"
	"criaisis/internal/handler"
	"criaisis/internal/router"
	"criaisis/internal/server"

	"github.com/rs/zerolog"
)

func testServer() *server.Server {
	logger := zerolog.Nop()
	return &server.Server{
		Config: &config.Config{
			Server: config.ServerConfig{
				Port:               "0",
				CORSAllowedOrigins: []string{"http://localhost:5173"},
			},
		},
		Logger: &logger,
	}
}

// A missing Slack signing secret must mount no Slack routes at all: fail
// closed by omission, never present-but-unverified (BR0.1/NFR1).
func TestNew_WithoutSlackInbound_Slack404s(t *testing.T) {
	h := router.New(testServer(), router.Handlers{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/slack/commands", strings.NewReader("command=/criaisis"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected /slack/commands to be absent (404) when SlackInbound is nil, got %d", rec.Code)
	}

	for _, path := range []string{"/slack/oauth/callback", "/slack/interactive", "/slack/events"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %s to be absent (404) when SlackInbound is nil, got %d", path, rec.Code)
		}
	}
}

// With a signature middleware configured, an unsigned request to a protected
// Slack route must be rejected by that middleware before reaching the handler.
func TestNew_WithSlackSignature_RejectsUnsigned(t *testing.T) {
	logger := zerolog.Nop()
	sig := handler.NewSlackSignatureMiddleware("test-signing-secret", &logger)

	h := router.New(testServer(), router.Handlers{
		SlackSignature: sig,
		SlackInbound:   &handler.SlackInboundHandler{},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/slack/commands", strings.NewReader("command=/criaisis"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected an unsigned request to be rejected, got 200 OK")
	}
}

func TestNew_HealthRoutesAlwaysMounted(t *testing.T) {
	h := router.New(testServer(), router.Handlers{
		Health: handler.NewHealthHandler(nil),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected /healthz to be mounted unconditionally, got %d", rec.Code)
	}
}
