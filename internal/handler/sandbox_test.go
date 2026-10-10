package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
	"criaisis/internal/handler"
	"criaisis/internal/service/sandbox"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestSandboxHandler(t *testing.T) {
	logger := zerolog.Nop()
	wsID := value.NewWorkspaceID()
	incRepo := newMockIncidentRepo()
	reproRepo := newMockReproRepo()
	prov := &mockProvisioner{block: make(chan struct{})}
	svc := sandbox.NewService(reproRepo, incRepo, prov, &logger)
	h := handler.NewSandboxHandler(svc, &logger)

	chanID, _ := value.ParseSlackChannelID("C123")
	threadTS, _ := value.ParseSlackThreadTS("123.456")
	userID, _ := value.ParseSlackUserID("U123")

	resolvedInc, _ := entity.NewIncident(wsID, "Resolved", "desc", chanID, threadTS, value.SeveritySev3, userID, nil)
	_ = resolvedInc.Resolve(time.Now().UTC())
	_ = resolvedInc.SetIncidentContext(value.NewIncidentContext(
		"api",
		"",
		[]string{"137 OOMKilled"},
		nil,
		nil,
	))
	_ = incRepo.Create(context.Background(), resolvedInc)

	ws, _ := entity.NewWorkspace("T123", "Acme", []byte("enc-token"))
	ws, _ = entity.ReconstituteWorkspace(wsID, "T123", "Acme", []byte("enc-token"), nil, nil, time.Now(), time.Now())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Inject authenticated workspace
			ctx := handler.WithWorkspace(r.Context(), ws)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/incidents/{incidentId}/sandbox", h.Trigger)
	r.Get("/incidents/{incidentId}/sandbox/{reproductionId}", h.Get)
	r.Get("/incidents/{incidentId}/sandbox/{reproductionId}/access", h.GetAccess)
	r.Post("/incidents/{incidentId}/sandbox/{reproductionId}/select-scenario", h.SelectScenario)

	var reproID string

	t.Run("POST /sandbox triggers reproduction", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/incidents/"+resolvedInc.ID().String()+"/sandbox", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d (body: %s)", rec.Code, rec.Body.String())
		}

		var res map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		reproID = res["id"].(string)
		if reproID == "" {
			t.Error("expected non-empty reproduction id")
		}
	})

	t.Run("GET /sandbox/{id} returns status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/incidents/"+resolvedInc.ID().String()+"/sandbox/"+reproID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("GET /sandbox/{id}/access returns 409 when not ready", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/incidents/"+resolvedInc.ID().String()+"/sandbox/"+reproID+"/access", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}
	})

	t.Run("GET /sandbox/{id}/access returns 200 when ready", func(t *testing.T) {
		close(prov.block)
		for i := 0; i < 30; i++ {
			time.Sleep(10 * time.Millisecond)
			rUUID, _ := uuid.Parse(reproID)
			if s, _ := reproRepo.GetByID(context.Background(), rUUID); s.Status() == value.SandboxStatusReady {
				break
			}
		}

		req := httptest.NewRequest("GET", "/incidents/"+resolvedInc.ID().String()+"/sandbox/"+reproID+"/access", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var access sandbox.AccessDetails
		_ = json.Unmarshal(rec.Body.Bytes(), &access)
		if access.ContainerRef != "docker://container-oom_crashloop" {
			t.Errorf("got ref %s, want docker://container-oom_crashloop", access.ContainerRef)
		}
	})

	t.Run("POST /sandbox/{id}/select-scenario validates input", func(t *testing.T) {
		body := bytes.NewReader([]byte(`{"scenario_id":"invalid_scenario"}`))
		req := httptest.NewRequest("POST", "/incidents/"+resolvedInc.ID().String()+"/sandbox/"+reproID+"/select-scenario", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}
