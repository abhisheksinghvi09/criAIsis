// Package router maps HTTP routes onto handlers and applies the middleware chain.
package router

import (
	"net/http"
	"time"

	"criaisis/internal/handler"
	"criaisis/internal/server"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// requestTimeout bounds any single HTTP request. Ingress only persists and queues,
// so nothing on this path legitimately takes longer.
const requestTimeout = 20 * time.Second

// Handlers bundles the HTTP entry points the router needs.
type Handlers struct {
	Alert          *handler.AlertHandler
	Workspace      *handler.WorkspaceHandler
	Runbook        *handler.RunbookHandler
	Incidents      *handler.IncidentReadHandler
	Personas       *handler.PersonaHandler
	Health         *handler.HealthHandler
	Auth           *handler.Auth
	SlackSignature *handler.SlackSignatureMiddleware
	SlackRateLimit *handler.WorkspaceRateLimitMiddleware
	SlackInbound   *handler.SlackInboundHandler
	Sandbox        *handler.SandboxHandler
}

// New builds the application router.
func New(srv *server.Server, h Handlers) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(requestTimeout))
	r.Use(corsMiddleware(srv.Config.Server.CORSAllowedOrigins))

	r.Get("/healthz", h.Health.Live)
	r.Get("/readyz", h.Health.Ready)

	// Inbound Slack surface (OAuth, Slash Commands, Interactivity, Events API)
	if h.SlackInbound != nil {
		r.Route("/slack", func(sr chi.Router) {
			sr.Get("/oauth/callback", h.SlackInbound.HandleOAuthCallback)

			sr.Group(func(protected chi.Router) {
				if h.SlackSignature != nil {
					protected.Use(h.SlackSignature.Handler)
				}
				if h.SlackRateLimit != nil {
					protected.Use(h.SlackRateLimit.Handler)
				}
				protected.Post("/commands", h.SlackInbound.HandleCommand)
				protected.Post("/interactive", h.SlackInbound.HandleInteractivity)
				protected.Post("/events", h.SlackInbound.HandleEvents)
			})
		})
	}

	r.Route("/api/v1", func(api chi.Router) {
		// Alert ingestion authenticates with the workspace's alert token, which is
		// deliberately a different credential from the admin key: a monitoring
		// system may open incidents but must not read or rewrite tenant keys.
		api.Route("/integrations/alerts", func(alerts chi.Router) {
			alerts.Post("/{provider}", h.Alert.Receive)
		})

		api.Route("/workspaces", func(ws chi.Router) {
			// Creating a tenant is a platform operation, not a tenant one.
			ws.With(h.Auth.RequirePlatformAdmin).Post("/", h.Workspace.Create)

			// Everything below is scoped to one tenant, and the admin key is checked
			// against the workspace named in the path: a valid key for another
			// tenant is rejected here.
			ws.Route("/{workspace}", func(one chi.Router) {
				one.Use(h.Auth.RequireWorkspaceAdmin)

				one.Get("/", h.Workspace.Show)
				one.Put("/llm", h.Workspace.SetLLM)
				one.Put("/embeddings", h.Workspace.SetEmbeddings)
				one.Put("/notifications", h.Workspace.SetNotifications)
				one.Post("/notifications/test", h.Workspace.TestNotification)
				one.Post("/test-investigation", h.Workspace.RunTestInvestigation)
				one.Post("/alert-token/rotate", h.Workspace.RotateAlertToken)

				one.Get("/runbooks", h.Runbook.List)
				one.Post("/runbooks", h.Runbook.Upload)

				one.Get("/incidents", h.Incidents.List)
				one.Get("/incidents/{incident}", h.Incidents.Show)

				if h.Sandbox != nil {
					one.Route("/incidents/{incidentId}/sandbox", func(sb chi.Router) {
						sb.Post("/", h.Sandbox.Trigger)
						sb.Get("/{reproductionId}", h.Sandbox.Get)
						sb.Get("/{reproductionId}/access", h.Sandbox.GetAccess)
						sb.Post("/{reproductionId}/select-scenario", h.Sandbox.SelectScenario)
					})
				}

				one.Get("/specialists", h.Personas.List)
				one.Patch("/specialists/{persona}", h.Personas.Update)
			})
		})
	})

	return r
}

// corsMiddleware permits only the configured origins. An empty allow-list denies
// cross-origin requests rather than defaulting to permissive.
func corsMiddleware(allowed []string) func(http.Handler) http.Handler {
	permitted := make(map[string]bool, len(allowed))
	for _, origin := range allowed {
		permitted[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin := r.Header.Get("Origin"); origin != "" && permitted[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Criaisis-Token")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
