package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"criaisis/internal/service/sandbox"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// SandboxHandler exposes sandbox reproduction operations over REST.
type SandboxHandler struct {
	service *sandbox.Service
	log     *zerolog.Logger
}

// NewSandboxHandler initializes the sandbox handler.
func NewSandboxHandler(service *sandbox.Service, log *zerolog.Logger) *SandboxHandler {
	return &SandboxHandler{service: service, log: log}
}

// Trigger handles POST /api/v1/incidents/{incidentId}/sandbox
func (h *SandboxHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	incIDStr := chi.URLParam(r, "incidentId")
	incID, err := uuid.Parse(incIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid incident id"})
		return
	}

	sr, err := h.service.Trigger(r.Context(), ws.ID(), incID)
	if err != nil {
		if errors.Is(err, sandbox.ErrIncidentNotResolved) || errors.Is(err, sandbox.ErrActiveReproductionExists) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		h.log.Error().Err(err).Msg("triggering sandbox reproduction failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed triggering reproduction"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":          sr.ID(),
		"incident_id": sr.IncidentID(),
		"status":      sr.Status().String(),
		"scenario_id": sr.ScenarioID(),
		"created_at":  sr.CreatedAt(),
	})
}

// Get handles GET /api/v1/incidents/{incidentId}/sandbox/{reproductionId}
func (h *SandboxHandler) Get(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	reprIDStr := chi.URLParam(r, "reproductionId")
	reprID, err := uuid.Parse(reprIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reproduction id"})
		return
	}

	sr, err := h.service.Get(r.Context(), ws.ID(), reprID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "reproduction not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":            sr.ID(),
		"incident_id":   sr.IncidentID(),
		"status":        sr.Status().String(),
		"scenario_id":   sr.ScenarioID(),
		"container_ref": sr.ContainerRef(),
		"created_at":    sr.CreatedAt(),
		"ready_at":      sr.ReadyAt(),
	})
}

// GetAccess handles GET /api/v1/incidents/{incidentId}/sandbox/{reproductionId}/access
func (h *SandboxHandler) GetAccess(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	reprIDStr := chi.URLParam(r, "reproductionId")
	reprID, err := uuid.Parse(reprIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reproduction id"})
		return
	}

	access, err := h.service.GetAccess(r.Context(), ws.ID(), reprID)
	if err != nil {
		if errors.Is(err, sandbox.ErrSandboxNotReady) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "reproduction is not ready yet"})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "reproduction not found"})
		return
	}

	writeJSON(w, http.StatusOK, access)
}

// SelectScenario handles POST /api/v1/incidents/{incidentId}/sandbox/{reproductionId}/select-scenario
func (h *SandboxHandler) SelectScenario(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	reprIDStr := chi.URLParam(r, "reproductionId")
	reprID, err := uuid.Parse(reprIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reproduction id"})
		return
	}

	var req struct {
		ScenarioID string `json:"scenario_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ScenarioID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scenario_id is required"})
		return
	}

	sr, err := h.service.SelectScenario(r.Context(), ws.ID(), reprID, req.ScenarioID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":          sr.ID(),
		"incident_id": sr.IncidentID(),
		"status":      sr.Status().String(),
		"scenario_id": sr.ScenarioID(),
	})
}
