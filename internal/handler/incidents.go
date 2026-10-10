package handler

import (
	"net/http"
	"strconv"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// defaultPageSize bounds an incident listing.
const defaultPageSize = 25

// IncidentReadHandler serves incident history and transcripts to the dashboard.
type IncidentReadHandler struct {
	incidents repository.IncidentRepository
	turns     repository.DebateTurnRepository
	personas  repository.PersonaRepository
	log       *zerolog.Logger
}

// NewIncidentReadHandler wires the read side of incidents.
func NewIncidentReadHandler(
	incidents repository.IncidentRepository,
	turns repository.DebateTurnRepository,
	personas repository.PersonaRepository,
	log *zerolog.Logger,
) *IncidentReadHandler {
	return &IncidentReadHandler{incidents: incidents, turns: turns, personas: personas, log: log}
}

// List returns recent incidents for the authenticated tenant only.
func (h *IncidentReadHandler) List(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	limit := defaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	incidents, err := h.incidents.ListRecent(r.Context(), ws.ID(), limit, 0)
	if err != nil {
		h.log.Error().Err(err).Msg("listing incidents")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list incidents"})
		return
	}

	summaries := make([]map[string]any, 0, len(incidents))
	for _, inc := range incidents {
		summaries = append(summaries, summarizeIncident(inc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": summaries})
}

// Show returns one incident with its full debate transcript.
func (h *IncidentReadHandler) Show(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "incident"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid incident id"})
		return
	}

	// Scoped by workspace, so one tenant cannot read another's transcript by id.
	inc, err := h.incidents.GetByID(r.Context(), ws.ID(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
		return
	}

	turns, err := h.turns.ListByIncident(r.Context(), ws.ID(), inc.ID())
	if err != nil {
		h.log.Error().Err(err).Msg("loading transcript")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load transcript"})
		return
	}

	names, err := h.personaNames(r, ws.ID())
	if err != nil {
		h.log.Error().Err(err).Msg("loading specialists")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load specialists"})
		return
	}

	rendered := make([]map[string]any, 0, len(turns))
	for _, turn := range turns {
		rendered = append(rendered, renderTurn(turn, names))
	}

	payload := summarizeIncident(inc)
	payload["transcript"] = rendered
	writeJSON(w, http.StatusOK, payload)
}

// personaNames maps persona ids to display names for transcript rendering.
func (h *IncidentReadHandler) personaNames(r *http.Request, wsID value.WorkspaceID) (map[uuid.UUID]string, error) {
	personas, err := h.personas.ListByWorkspace(r.Context(), wsID)
	if err != nil {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(personas))
	for _, persona := range personas {
		names[persona.ID()] = persona.DisplayName()
	}
	return names, nil
}

// summarizeIncident renders an incident for a list row or a detail header.
func summarizeIncident(inc *entity.Incident) map[string]any {
	payload := map[string]any{
		"id":          inc.ID().String(),
		"title":       inc.Title(),
		"description": inc.Description(),
		"severity":    inc.Severity().String(),
		"status":      inc.Status().String(),
		"trigger":     inc.TriggerType().String(),
		"created_at":  inc.CreatedAt(),
	}
	if resolved := inc.ResolvedAt(); resolved != nil {
		payload["resolved_at"] = *resolved
	}
	if ictx, err := inc.IncidentContext(); err == nil && ictx != nil && !ictx.IsEmpty() {
		payload["context"] = map[string]any{
			"provider":     ictx.Provider(),
			"alert_name":   ictx.AlertName(),
			"error_logs":   ictx.ErrorLogs(),
			"stack_traces": ictx.StackTraces(),
			"metrics":      ictx.Metrics(),
		}
	}
	return payload
}

// renderTurn renders one debate turn with its citations resolved.
func renderTurn(turn *entity.DebateTurn, names map[uuid.UUID]string) map[string]any {
	payload := map[string]any{
		"id":         turn.ID().String(),
		"stage":      turn.Stage().Int(),
		"turn_type":  turn.TurnType().String(),
		"content":    turn.Content(),
		"created_at": turn.CreatedAt(),
		"metadata":   turn.Metadata(),
	}
	if personaID := turn.PersonaID(); personaID != nil {
		payload["persona"] = names[*personaID]
	}
	if citations, err := turn.Citations(); err == nil {
		payload["citations"] = citations
	}
	return payload
}
