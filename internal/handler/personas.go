package handler

import (
	"net/http"

	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// PersonaHandler lets a tenant tune their four specialists.
type PersonaHandler struct {
	personas  repository.PersonaRepository
	documents repository.DocumentRepository
	log       *zerolog.Logger
}

// NewPersonaHandler wires specialist management.
func NewPersonaHandler(personas repository.PersonaRepository, documents repository.DocumentRepository, log *zerolog.Logger) *PersonaHandler {
	return &PersonaHandler{personas: personas, documents: documents, log: log}
}

// List returns the tenant's specialists with their prompts and runbook counts.
func (h *PersonaHandler) List(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	personas, err := h.personas.ListByWorkspace(r.Context(), ws.ID())
	if err != nil {
		h.log.Error().Err(err).Msg("listing specialists")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list specialists"})
		return
	}

	rendered := make([]map[string]any, 0, len(personas))
	for _, persona := range personas {
		docs, err := h.documents.ListByPersona(r.Context(), ws.ID(), persona.ID())
		if err != nil {
			h.log.Error().Err(err).Msg("counting runbooks")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not count runbooks"})
			return
		}
		var chunks int
		for _, doc := range docs {
			chunks += doc.ChunkCount()
		}
		rendered = append(rendered, map[string]any{
			"key": persona.Key().String(), "display_name": persona.DisplayName(),
			"system_prompt": persona.SystemPrompt(), "enabled": persona.IsEnabled(),
			"runbooks": len(docs), "chunks": chunks,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"specialists": rendered})
}

type personaUpdate struct {
	SystemPrompt *string `json:"system_prompt"`
	Enabled      *bool   `json:"enabled"`
}

// Update edits one specialist's prompt or participation.
//
// The read-only mandate is prepended by the engine and is not part of this field,
// so a tenant cannot edit away the safety boundary.
func (h *PersonaHandler) Update(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	key, err := value.ParsePersonaKey(chi.URLParam(r, "persona"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var req personaUpdate
	if !decodeJSON(w, r, &req) {
		return
	}

	persona, err := h.personas.GetByKey(r.Context(), ws.ID(), key)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "specialist not found"})
		return
	}

	if req.SystemPrompt != nil {
		if err := persona.UpdatePrompt(*req.SystemPrompt); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.Enabled != nil {
		if *req.Enabled {
			persona.Enable()
		} else {
			persona.Disable()
		}
	}

	if err := h.personas.Update(r.Context(), persona); err != nil {
		h.log.Error().Err(err).Msg("updating specialist")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update specialist"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"key": persona.Key().String(), "display_name": persona.DisplayName(),
		"system_prompt": persona.SystemPrompt(), "enabled": persona.IsEnabled(),
	})
}
