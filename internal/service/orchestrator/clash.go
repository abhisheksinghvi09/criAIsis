package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/llm"
	"criaisis/internal/infrastructure/telemetry"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

// retrievalTopK is how many runbook excerpts each specialist is grounded in.
// Wide enough to cover a procedure, narrow enough to keep Stage 1 inside its budget.
const retrievalTopK = 5

// Orchestrator runs the 2-Stage Asynchronous Clash over a persisted incident.
type Orchestrator struct {
	personas repository.PersonaRepository
	chunks   repository.ChunkRepository
	turns    repository.DebateTurnRepository
	chat     llm.ChatProvider
	embedder llm.EmbeddingProvider
	tools    *telemetry.Registry
	log      *zerolog.Logger
}

// New wires the clash engine to its persistence, model, and telemetry dependencies.
func New(
	personas repository.PersonaRepository,
	chunks repository.ChunkRepository,
	turns repository.DebateTurnRepository,
	chat llm.ChatProvider,
	embedder llm.EmbeddingProvider,
	tools *telemetry.Registry,
	log *zerolog.Logger,
) *Orchestrator {
	return &Orchestrator{
		personas: personas, chunks: chunks, turns: turns,
		chat: chat, embedder: embedder, tools: tools, log: log,
	}
}

// SpecialistOutcome pairs a persona with its Stage 1 result, or the reason it produced none.
type SpecialistOutcome struct {
	Persona *entity.Persona
	Result  SpecialistResult
	Turn    *entity.DebateTurn
	Err     error
}

// ClashResult is the full transcript of one 2-stage investigation.
type ClashResult struct {
	Stage1        []SpecialistOutcome
	Synthesis     SynthesisResult
	SynthesisTurn *entity.DebateTurn
}

// SucceededCount reports how many specialists filed a hypothesis.
func (r ClashResult) SucceededCount() int { return succeeded(r.Stage1) }

// RunClash executes Stage 1 and Stage 2 and persists every turn.
//
// Stage 1 degrades gracefully by design: a persona whose model call or retrieval
// fails is recorded as a failed outcome and the remaining specialists and the
// consensus still post. Only a total Stage 1 washout aborts the clash.
func (o *Orchestrator) RunClash(ctx context.Context, inc *entity.Incident) (*ClashResult, error) {
	personas, err := o.enabledPersonas(ctx, inc.WorkspaceID())
	if err != nil {
		return nil, err
	}

	outcomes, err := o.runStage1(ctx, inc, personas)
	if err != nil {
		return nil, err
	}
	if succeeded(outcomes) == 0 {
		return nil, errors.New("clash aborted: every specialist failed to produce a hypothesis")
	}

	synthesis, turn, err := o.runStage2(ctx, inc, outcomes)
	if err != nil {
		return nil, err
	}

	return &ClashResult{Stage1: outcomes, Synthesis: synthesis, SynthesisTurn: turn}, nil
}

// enabledPersonas loads the specialists configured to participate in this workspace.
func (o *Orchestrator) enabledPersonas(ctx context.Context, wsID value.WorkspaceID) ([]*entity.Persona, error) {
	all, err := o.personas.ListByWorkspace(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("loading personas: %w", err)
	}

	enabled := make([]*entity.Persona, 0, len(all))
	for _, persona := range all {
		if persona.IsEnabled() {
			enabled = append(enabled, persona)
		}
	}
	if len(enabled) == 0 {
		return nil, errors.New("workspace has no enabled personas")
	}
	return enabled, nil
}

// runStage1 fans the incident out to every enabled specialist concurrently.
func (o *Orchestrator) runStage1(ctx context.Context, inc *entity.Incident, personas []*entity.Persona) ([]SpecialistOutcome, error) {
	query := retrievalQuery(inc)
	embeddings, err := o.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embedding incident query: %w", err)
	}
	if len(embeddings) != 1 {
		return nil, fmt.Errorf("embedding incident query: got %d vectors", len(embeddings))
	}

	outcomes := make([]SpecialistOutcome, len(personas))
	var group errgroup.Group
	for i, persona := range personas {
		outcomes[i].Persona = persona
		group.Go(func() error {
			// Never propagate: one failed specialist must not cancel its siblings.
			result, turn, err := o.runSpecialist(ctx, inc, persona, query, embeddings[0])
			outcomes[i].Result, outcomes[i].Turn, outcomes[i].Err = result, turn, err
			if err != nil {
				o.log.Warn().Err(err).Str("persona", persona.Key().String()).Msg("specialist hypothesis failed")
			}
			return nil
		})
	}
	_ = group.Wait()

	return outcomes, nil
}

// runSpecialist grounds one persona in its own runbooks and records its hypothesis.
func (o *Orchestrator) runSpecialist(
	ctx context.Context,
	inc *entity.Incident,
	persona *entity.Persona,
	query string,
	queryEmbedding value.EmbeddingVector,
) (SpecialistResult, *entity.DebateTurn, error) {
	var empty SpecialistResult

	results, err := o.chunks.SearchHybrid(ctx, inc.WorkspaceID(), persona.ID(), query, queryEmbedding, retrievalTopK)
	if err != nil {
		return empty, nil, fmt.Errorf("retrieving %s runbooks: %w", persona.Key(), err)
	}

	raw, err := o.chat.Complete(ctx, llm.ChatRequest{
		Role:   llm.RoleSpecialist,
		System: personaSystemPrompt(persona),
		User:   specialistPrompt(inc, results, nil),
		Schema: specialistSchema(),
	})
	if err != nil {
		return empty, nil, err
	}

	var result SpecialistResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return empty, nil, fmt.Errorf("parsing %s hypothesis: %w", persona.Key(), err)
	}

	citations, chunkIDs := resolveCitations(result.CitedChunkIDs, results)
	personaID := persona.ID()
	turn, err := entity.NewDebateTurn(
		inc.ID(), inc.WorkspaceID(), &personaID,
		value.StageSpecialistBlast, value.TurnTypeSpecialistHypothesis,
		result.Hypothesis, chunkIDs, "",
		turnMetadata{
			Citations:         citations,
			Confidence:        result.Confidence,
			DiagnosticQueries: result.DiagnosticQueries,
		}.JSON(),
	)
	if err != nil {
		return empty, nil, fmt.Errorf("building %s turn: %w", persona.Key(), err)
	}
	if err := o.turns.Create(ctx, turn); err != nil {
		return empty, nil, fmt.Errorf("persisting %s turn: %w", persona.Key(), err)
	}
	return result, turn, nil
}

// runStage2 cross-examines the Stage 1 hypotheses into a single consensus verdict.
func (o *Orchestrator) runStage2(ctx context.Context, inc *entity.Incident, outcomes []SpecialistOutcome) (SynthesisResult, *entity.DebateTurn, error) {
	var empty SynthesisResult

	raw, err := o.chat.Complete(ctx, llm.ChatRequest{
		Role:   llm.RoleSynthesis,
		System: synthesisSystemPrompt(),
		User:   synthesisPrompt(inc, outcomes),
		Schema: synthesisSchema(),
	})
	if err != nil {
		return empty, nil, fmt.Errorf("consensus synthesis: %w", err)
	}

	var result SynthesisResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return empty, nil, fmt.Errorf("parsing consensus synthesis: %w", err)
	}
	if _, err := result.ParsedClassification(); err != nil {
		return empty, nil, fmt.Errorf("consensus synthesis: %w", err)
	}

	citations, chunkIDs := filterCitations(result.CitedChunkIDs, stage1Citations(outcomes))
	turn, err := entity.NewDebateTurn(
		inc.ID(), inc.WorkspaceID(), nil,
		value.StageConsensusSynthesis, value.TurnTypeSynthesis,
		result.Consensus, chunkIDs, "",
		turnMetadata{
			Citations:         citations,
			Classification:    result.Classification,
			OwningDomain:      result.OwningDomain,
			Contradictions:    result.Contradictions,
			NextSteps:         result.NextSteps,
			DiagnosticQueries: result.DiagnosticQueries,
		}.JSON(),
	)
	if err != nil {
		return empty, nil, fmt.Errorf("building synthesis turn: %w", err)
	}
	if err := o.turns.Create(ctx, turn); err != nil {
		return empty, nil, fmt.Errorf("persisting synthesis turn: %w", err)
	}
	return result, turn, nil
}

// FollowUp answers an in-thread question directed at one specialist (Stage 3).
func (o *Orchestrator) FollowUp(ctx context.Context, inc *entity.Incident, key value.PersonaKey, question string) (*entity.DebateTurn, error) {
	persona, err := o.personas.GetByKey(ctx, inc.WorkspaceID(), key)
	if err != nil {
		return nil, fmt.Errorf("loading %s persona: %w", key, err)
	}

	embeddings, err := o.embedder.Embed(ctx, []string{question})
	if err != nil {
		return nil, fmt.Errorf("embedding follow-up question: %w", err)
	}

	results, err := o.chunks.SearchHybrid(ctx, inc.WorkspaceID(), persona.ID(), question, embeddings[0], retrievalTopK)
	if err != nil {
		return nil, fmt.Errorf("retrieving %s runbooks: %w", key, err)
	}

	transcript, err := o.turns.ListByIncident(ctx, inc.WorkspaceID(), inc.ID())
	if err != nil {
		return nil, fmt.Errorf("loading transcript: %w", err)
	}

	answer, err := o.chat.Complete(ctx, llm.ChatRequest{
		Role:   llm.RoleSpecialist,
		System: personaSystemPrompt(persona),
		User:   followUpPrompt(inc, question, transcript, results),
	})
	if err != nil {
		return nil, fmt.Errorf("%s follow-up: %w", key, err)
	}

	citations, chunkIDs := allCitations(results)
	personaID := persona.ID()
	turn, err := entity.NewDebateTurn(
		inc.ID(), inc.WorkspaceID(), &personaID,
		value.StageInteractiveFollowUp, value.TurnTypeFollowUp,
		answer, chunkIDs, "",
		turnMetadata{Citations: citations}.JSON(),
	)
	if err != nil {
		return nil, fmt.Errorf("building follow-up turn: %w", err)
	}
	if err := o.turns.Create(ctx, turn); err != nil {
		return nil, fmt.Errorf("persisting follow-up turn: %w", err)
	}
	return turn, nil
}

// retrievalQuery builds the text used for hybrid search, combining the incident
// narrative with the ingested telemetry so keyword search can match log lines verbatim.
func retrievalQuery(inc *entity.Incident) string {
	query := inc.Title() + "\n" + inc.Description()

	ctx, err := inc.IncidentContext()
	if err != nil || ctx == nil {
		return query
	}
	for _, line := range ctx.ErrorLogs() {
		query += "\n" + line
	}
	for _, trace := range ctx.StackTraces() {
		query += "\n" + trace
	}
	return query
}

// resolveCitations keeps only the chunk IDs the model was actually shown, guarding
// the transcript against hallucinated or stale references.
func resolveCitations(cited []string, available []*repository.SearchResult) ([]value.CitationSnapshot, []uuid.UUID) {
	byID := make(map[uuid.UUID]*repository.SearchResult, len(available))
	for _, res := range available {
		byID[res.Chunk.ID()] = res
	}

	var (
		snapshots []value.CitationSnapshot
		ids       []uuid.UUID
		seen      = make(map[uuid.UUID]bool)
	)
	for _, raw := range cited {
		id, err := uuid.Parse(raw)
		if err != nil || seen[id] {
			continue
		}
		res, ok := byID[id]
		if !ok {
			continue
		}
		snapshot, err := value.NewCitationSnapshot(id, res.DocumentTitle, res.Chunk.ChunkText(), "")
		if err != nil {
			continue
		}
		seen[id] = true
		snapshots = append(snapshots, snapshot)
		ids = append(ids, id)
	}
	return snapshots, ids
}

// allCitations snapshots every retrieved excerpt, used where the model is not asked
// to enumerate its own citations.
func allCitations(results []*repository.SearchResult) ([]value.CitationSnapshot, []uuid.UUID) {
	ids := make([]string, 0, len(results))
	for _, res := range results {
		ids = append(ids, res.Chunk.ID().String())
	}
	return resolveCitations(ids, results)
}

// stage1Citations indexes every snapshot captured during Stage 1. Retrieval happened
// per persona, so the synthesis turn cites this pooled evidence rather than running
// a fifth search of its own.
func stage1Citations(outcomes []SpecialistOutcome) map[uuid.UUID]value.CitationSnapshot {
	pooled := make(map[uuid.UUID]value.CitationSnapshot)
	for _, outcome := range outcomes {
		if outcome.Err != nil || outcome.Turn == nil {
			continue
		}
		snapshots, err := outcome.Turn.Citations()
		if err != nil {
			continue
		}
		for _, snapshot := range snapshots {
			pooled[snapshot.ChunkID] = snapshot
		}
	}
	return pooled
}

// filterCitations keeps only the synthesis citations that a specialist actually
// captured, so the consensus can never cite evidence nobody was shown.
func filterCitations(cited []string, pooled map[uuid.UUID]value.CitationSnapshot) ([]value.CitationSnapshot, []uuid.UUID) {
	var (
		snapshots []value.CitationSnapshot
		ids       []uuid.UUID
		seen      = make(map[uuid.UUID]bool)
	)
	for _, raw := range cited {
		id, err := uuid.Parse(raw)
		if err != nil || seen[id] {
			continue
		}
		snapshot, ok := pooled[id]
		if !ok {
			continue
		}
		seen[id] = true
		snapshots = append(snapshots, snapshot)
		ids = append(ids, id)
	}
	return snapshots, ids
}

// succeeded counts specialists that filed a hypothesis.
func succeeded(outcomes []SpecialistOutcome) int {
	var n int
	for _, outcome := range outcomes {
		if outcome.Err == nil {
			n++
		}
	}
	return n
}
