package orchestrator_test

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/llm"
	"criaisis/internal/infrastructure/telemetry"
	"criaisis/internal/service/orchestrator"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fixture struct {
	orch     *orchestrator.Orchestrator
	incident *entity.Incident
	turns    *memTurns
	personas *memPersonas
	chunks   *memChunks
}

// newFixture builds a workspace with all four personas, one retrievable runbook
// excerpt each, and the deterministic offline chat stub.
func newFixture(t *testing.T, chat llm.ChatProvider) *fixture {
	t.Helper()

	wsID := value.NewWorkspaceID()
	personas := &memPersonas{}
	chunks := &memChunks{byPersona: map[uuid.UUID][]*repository.SearchResult{}, failFor: map[uuid.UUID]bool{}}

	for _, key := range value.AllPersonaKeys() {
		persona, err := entity.NewPersona(wsID, key, key.String()+" specialist", "You are the "+key.String()+" specialist.")
		if err != nil {
			t.Fatalf("building persona %s: %v", key, err)
		}
		personas.personas = append(personas.personas, persona)
		chunks.byPersona[persona.ID()] = []*repository.SearchResult{
			newSearchResult(t, wsID, persona.ID(), key.String()+" runbook excerpt about connection saturation"),
		}
	}

	incident := newIncident(t, wsID)
	turns := &memTurns{}
	logger := zerolog.New(io.Discard)

	if chat == nil {
		chat = orchestrator.NewStubChat()
	}

	return &fixture{
		orch:     orchestrator.New(personas, chunks, turns, chat, llm.FakeEmbedder{}, telemetry.NewRegistry(), &logger),
		incident: incident,
		turns:    turns,
		personas: personas,
		chunks:   chunks,
	}
}

func newIncident(t *testing.T, wsID value.WorkspaceID) *entity.Incident {
	t.Helper()
	channel, _ := value.NewSlackChannelID("C123456")
	thread, _ := value.NewSlackThreadTS("1726531200.000100")
	user, _ := value.NewSlackUserID("U123456")

	inc, err := entity.NewIncident(wsID, "Checkout 5xx spike", "Connections exhausted", channel, thread, value.SeveritySev1, user, nil)
	if err != nil {
		t.Fatalf("building incident: %v", err)
	}
	return inc
}

func newSearchResult(t *testing.T, wsID value.WorkspaceID, personaID uuid.UUID, text string) *repository.SearchResult {
	t.Helper()
	embedding, err := llm.FakeEmbedder{}.Embed(context.Background(), []string{text})
	if err != nil {
		t.Fatalf("embedding chunk: %v", err)
	}
	chunk, err := entity.NewDocumentChunk(wsID, personaID, uuid.New(), 0, text, 12, embedding[0], nil)
	if err != nil {
		t.Fatalf("building chunk: %v", err)
	}
	return &repository.SearchResult{Chunk: chunk, Score: 1, DocumentTitle: "Runbook"}
}

func TestRunClash_PersistsEveryStageTurn(t *testing.T) {
	f := newFixture(t, nil)

	result, err := f.orch.RunClash(context.Background(), f.incident)
	if err != nil {
		t.Fatalf("clash failed: %v", err)
	}

	if got := f.turns.stageCount(value.StageSpecialistBlast); got != 4 {
		t.Errorf("expected 4 specialist turns, got %d", got)
	}
	if got := f.turns.stageCount(value.StageConsensusSynthesis); got != 1 {
		t.Errorf("expected 1 synthesis turn, got %d", got)
	}
	if result.SucceededCount() != 4 {
		t.Errorf("expected 4 successful specialists, got %d", result.SucceededCount())
	}
	if _, err := result.Synthesis.ParsedClassification(); err != nil {
		t.Errorf("synthesis classification did not parse: %v", err)
	}
}

// Each specialist must be grounded only in its own persona's excerpts (FR-8).
func TestRunClash_CitesOnlyOwnPersonaChunks(t *testing.T) {
	f := newFixture(t, nil)

	if _, err := f.orch.RunClash(context.Background(), f.incident); err != nil {
		t.Fatalf("clash failed: %v", err)
	}

	for _, turn := range f.turns.all() {
		if turn.Stage() != value.StageSpecialistBlast {
			continue
		}
		citations, err := turn.Citations()
		if err != nil {
			t.Fatalf("reading citations: %v", err)
		}
		if len(citations) == 0 {
			t.Fatal("expected a specialist turn to cite its runbook excerpt")
		}
		own := f.chunks.byPersona[*turn.PersonaID()]
		for _, citation := range citations {
			if citation.ChunkID != own[0].Chunk.ID() {
				t.Errorf("persona cited a chunk outside its own runbooks: %s", citation.ChunkID)
			}
		}
	}
}

// The synthesis turn must carry the Stage 1 evidence forward, or post-mortems lose
// the grounding behind the verdict (FR-10).
func TestRunClash_SynthesisCarriesStage1Citations(t *testing.T) {
	f := newFixture(t, nil)

	result, err := f.orch.RunClash(context.Background(), f.incident)
	if err != nil {
		t.Fatalf("clash failed: %v", err)
	}

	citations, err := result.SynthesisTurn.Citations()
	if err != nil {
		t.Fatalf("reading synthesis citations: %v", err)
	}
	if len(citations) != 4 {
		t.Fatalf("expected the synthesis to cite all 4 specialist excerpts, got %d", len(citations))
	}
	if len(result.SynthesisTurn.ReferencedChunkIDs()) != 4 {
		t.Errorf("referenced chunk ids out of step with snapshots")
	}
}

// Graceful degradation (NFR): one specialist failing must not sink the clash.
func TestRunClash_SurvivesOneFailingSpecialist(t *testing.T) {
	f := newFixture(t, nil)
	f.chunks.failFor[f.personas.personas[0].ID()] = true

	result, err := f.orch.RunClash(context.Background(), f.incident)
	if err != nil {
		t.Fatalf("clash should survive a single specialist failure: %v", err)
	}

	if result.SucceededCount() != 3 {
		t.Errorf("expected 3 surviving specialists, got %d", result.SucceededCount())
	}
	if got := f.turns.stageCount(value.StageConsensusSynthesis); got != 1 {
		t.Errorf("consensus must still post, got %d synthesis turns", got)
	}
	if result.Stage1[0].Err == nil {
		t.Error("expected the failing specialist to record its error")
	}
}

func TestRunClash_AbortsWhenEverySpecialistFails(t *testing.T) {
	f := newFixture(t, nil)
	for _, persona := range f.personas.personas {
		f.chunks.failFor[persona.ID()] = true
	}

	if _, err := f.orch.RunClash(context.Background(), f.incident); err == nil {
		t.Fatal("expected the clash to abort when no specialist produces a hypothesis")
	}
}

// A model citing an excerpt it was never shown must not reach the transcript.
func TestRunClash_DropsHallucinatedCitations(t *testing.T) {
	hallucinating := llm.ChatFunc(func(_ context.Context, req llm.ChatRequest) (string, error) {
		if req.Role == llm.RoleSynthesis {
			return marshalJSON(t, orchestrator.SynthesisResult{
				Consensus: "c", Classification: "code", OwningDomain: "application",
				CitedChunkIDs: []string{uuid.New().String()},
			}), nil
		}
		return marshalJSON(t, orchestrator.SpecialistResult{
			Hypothesis:    "h",
			Confidence:    "high",
			CitedChunkIDs: []string{uuid.New().String()},
		}), nil
	})

	f := newFixture(t, hallucinating)
	result, err := f.orch.RunClash(context.Background(), f.incident)
	if err != nil {
		t.Fatalf("clash failed: %v", err)
	}

	for _, turn := range f.turns.all() {
		citations, err := turn.Citations()
		if err != nil {
			t.Fatalf("reading citations: %v", err)
		}
		if len(citations) != 0 {
			t.Errorf("stage %d turn kept a citation it was never shown", turn.Stage().Int())
		}
	}
	if len(result.SynthesisTurn.ReferencedChunkIDs()) != 0 {
		t.Error("synthesis kept a hallucinated chunk reference")
	}
}

func TestRunClash_RejectsUnparseableClassification(t *testing.T) {
	bogus := llm.ChatFunc(func(_ context.Context, req llm.ChatRequest) (string, error) {
		if req.Role == llm.RoleSynthesis {
			return `{"consensus":"c","classification":"vibes","owning_domain":"application"}`, nil
		}
		return orchestrator.NewStubChat().Complete(context.Background(), req)
	})

	f := newFixture(t, bogus)
	if _, err := f.orch.RunClash(context.Background(), f.incident); err == nil {
		t.Fatal("expected an invalid classification to fail the synthesis")
	}
}

func TestFollowUp_PersistsStage3Turn(t *testing.T) {
	f := newFixture(t, llm.ChatFunc(func(context.Context, llm.ChatRequest) (string, error) {
		return "Run pg_stat_activity and inspect idle in transaction sessions.", nil
	}))

	turn, err := f.orch.FollowUp(context.Background(), f.incident, value.PersonaKeyDatabase, "why are connections spiking?")
	if err != nil {
		t.Fatalf("follow-up failed: %v", err)
	}
	if turn.Stage() != value.StageInteractiveFollowUp {
		t.Errorf("expected stage 3, got %d", turn.Stage().Int())
	}
	if turn.TurnType() != value.TurnTypeFollowUp {
		t.Errorf("expected follow_up turn type, got %s", turn.TurnType())
	}
	if len(turn.ReferencedChunkIDs()) == 0 {
		t.Error("expected the follow-up to cite the persona's runbook excerpt")
	}
}

func TestFollowUp_PropagatesUnknownPersona(t *testing.T) {
	f := newFixture(t, nil)
	f.personas.personas = nil

	if _, err := f.orch.FollowUp(context.Background(), f.incident, value.PersonaKeyNetwork, "status?"); err == nil {
		t.Fatal("expected an error for a persona that is not configured")
	}
}

func marshalJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling test payload: %v", err)
	}
	return string(raw)
}
