package handler

import (
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/service/ingest"
	"criaisis/internal/service/tenant"

	"github.com/rs/zerolog"
)

// maxRunbookBytes caps an uploaded document. Runbooks are prose; far larger is a
// mistake, and chunking one would flood the tenant's embedding quota.
const maxRunbookBytes = 2 << 20

// RunbookHandler ingests a tenant's documentation for one specialist.
//
// Embedding runs on the tenant's own credential, so the cost and rate limit of
// ingestion belong to the customer whose documents these are.
type RunbookHandler struct {
	documents repository.DocumentRepository
	chunks    repository.ChunkRepository
	personas  repository.PersonaRepository
	resolver  *tenant.Resolver
	log       *zerolog.Logger
}

// NewRunbookHandler wires runbook ingestion.
func NewRunbookHandler(
	documents repository.DocumentRepository,
	chunks repository.ChunkRepository,
	personas repository.PersonaRepository,
	resolver *tenant.Resolver,
	log *zerolog.Logger,
) *RunbookHandler {
	return &RunbookHandler{documents: documents, chunks: chunks, personas: personas, resolver: resolver, log: log}
}

type runbookRequest struct {
	Persona string `json:"persona"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Upload ingests one runbook: chunk, embed, index. The document is searchable by
// the time this returns, so a customer can confirm coverage immediately.
func (h *RunbookHandler) Upload(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	var req runbookRequest
	// Headroom over the content cap covers JSON escaping and the other fields.
	if !decodeJSONLimit(w, r, &req, maxRunbookBytes+(64<<10)) {
		return
	}

	key, err := value.ParsePersonaKey(req.Persona)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Title == "" || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title and content are required"})
		return
	}
	if len(req.Content) > maxRunbookBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("runbook exceeds %d bytes; split it into sections", maxRunbookBytes),
		})
		return
	}

	persona, err := h.personas.GetByKey(r.Context(), ws.ID(), key)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "specialist not found for this workspace"})
		return
	}

	doc, err := entity.NewDocument(ws.ID(), persona.ID(), req.Title, req.Content)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Re-uploading unchanged content would duplicate every chunk and skew retrieval.
	if existing, err := h.documents.GetByContentHash(r.Context(), ws.ID(), doc.ContentHash()); err == nil && existing != nil {
		if existing.Status() == value.DocumentStatusIndexed {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "already ingested, unchanged", "title": existing.Title(), "chunks": existing.ChunkCount(),
			})
			return
		}
		// A failed or abandoned attempt has no usable chunks and must not block a
		// retry of the same content: clear it and ingest afresh.
		if err := h.documents.Delete(r.Context(), ws.ID(), existing.ID()); err != nil {
			h.log.Error().Err(err).Msg("clearing failed runbook before retry")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not retry ingestion"})
			return
		}
	}

	tenantEmbedder, err := h.resolver.ResolveEmbedder(r.Context(), ws.ID())
	if err != nil {
		h.log.Error().Err(err).Msg("resolving embedder")
		msg := "could not prepare embeddings for this workspace"
		if errors.Is(err, tenant.ErrNotConfigured) {
			msg = "set your embedding credential before uploading runbooks"
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}

	if err := ingest.NewService(h.documents, h.chunks, tenantEmbedder).IndexNew(r.Context(), doc); err != nil {
		h.log.Error().Err(err).Str("workspace", ws.SlackTeamID()).Msg("runbook ingestion failed")
		// Detail is in the log; the embedding provider's error text is not for the client.
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "ingestion failed; check your embedding credential and try again"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "indexed", "persona": key.String(), "title": doc.Title(), "chunks": doc.ChunkCount(),
	})
}

// List reports runbook coverage per specialist, so a customer can see the gaps
// that would make a persona report no confidence during an incident.
func (h *RunbookHandler) List(w http.ResponseWriter, r *http.Request) {
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

	docs, err := h.documents.ListSummaries(r.Context(), ws.ID())
	if err != nil {
		h.log.Error().Err(err).Msg("listing runbooks")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list runbooks"})
		return
	}
	byPersona := map[uuid.UUID][]repository.DocumentSummary{}
	for _, d := range docs {
		byPersona[d.PersonaID] = append(byPersona[d.PersonaID], d)
	}

	coverage := make([]map[string]any, 0, len(personas))
	for _, persona := range personas {
		own := byPersona[persona.ID()]
		titles := make([]string, 0, len(own))
		documents := make([]map[string]any, 0, len(own))
		var chunks int
		for _, d := range own {
			titles = append(titles, d.Title)
			chunks += d.ChunkCount
			documents = append(documents, map[string]any{
				"id": d.ID.String(), "title": d.Title, "status": d.Status, "chunks": d.ChunkCount,
			})
		}
		coverage = append(coverage, map[string]any{
			"persona": persona.Key().String(), "enabled": persona.IsEnabled(),
			"runbooks": len(own), "chunks": chunks, "titles": titles, "documents": documents,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"coverage": coverage})
}

// Delete removes a runbook and, through the foreign key, all of its chunks, so the
// specialist stops citing it on the next investigation.
func (h *RunbookHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "runbook"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid runbook id"})
		return
	}
	// Scoped lookup first: another tenant's id is indistinguishable from a missing one.
	if _, err := h.documents.GetByID(r.Context(), ws.ID(), id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "runbook not found"})
		return
	}
	if err := h.documents.Delete(r.Context(), ws.ID(), id); err != nil {
		h.log.Error().Err(err).Msg("deleting runbook")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not delete runbook"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
