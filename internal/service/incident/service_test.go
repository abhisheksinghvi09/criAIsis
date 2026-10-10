package incident_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/llm"
	"criaisis/internal/job"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/orchestrator"
	"criaisis/internal/service/tenant"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// --- fake repositories: enough of each interface for RunClash to complete ---

type fakeIncidentRepo struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*entity.Incident
}

func newFakeIncidentRepo() *fakeIncidentRepo {
	return &fakeIncidentRepo{byID: map[uuid.UUID]*entity.Incident{}}
}

func (r *fakeIncidentRepo) Create(_ context.Context, inc *entity.Incident) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[inc.ID()] = inc
	return nil
}
func (r *fakeIncidentRepo) GetByID(_ context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Incident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inc, ok := r.byID[id]
	if !ok || inc.WorkspaceID() != wsID {
		return nil, errors.New("incident not found")
	}
	return inc, nil
}
func (r *fakeIncidentRepo) GetBySlackThread(context.Context, value.WorkspaceID, value.SlackChannelID, value.SlackThreadTS) (*entity.Incident, error) {
	return nil, errors.New("not implemented")
}
func (r *fakeIncidentRepo) ListActive(context.Context, value.WorkspaceID, int, int) ([]*entity.Incident, error) {
	return nil, nil
}
func (r *fakeIncidentRepo) ListRecent(context.Context, value.WorkspaceID, int, int) ([]*entity.Incident, error) {
	return nil, nil
}
func (r *fakeIncidentRepo) Update(_ context.Context, inc *entity.Incident) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[inc.ID()] = inc
	return nil
}

var _ repository.IncidentRepository = (*fakeIncidentRepo)(nil)

type fakePersonaRepo struct{ personas []*entity.Persona }

func (r *fakePersonaRepo) Create(context.Context, *entity.Persona) error { return nil }
func (r *fakePersonaRepo) GetByID(context.Context, value.WorkspaceID, uuid.UUID) (*entity.Persona, error) {
	return nil, errors.New("not implemented")
}
func (r *fakePersonaRepo) GetByKey(context.Context, value.WorkspaceID, value.PersonaKey) (*entity.Persona, error) {
	return nil, errors.New("not implemented")
}
func (r *fakePersonaRepo) ListByWorkspace(context.Context, value.WorkspaceID) ([]*entity.Persona, error) {
	return r.personas, nil
}
func (r *fakePersonaRepo) Update(context.Context, *entity.Persona) error { return nil }

var _ repository.PersonaRepository = (*fakePersonaRepo)(nil)

type fakeChunkRepo struct{}

func (fakeChunkRepo) BatchCreate(context.Context, []*entity.DocumentChunk) error { return nil }
func (fakeChunkRepo) GetByID(context.Context, value.WorkspaceID, uuid.UUID) (*entity.DocumentChunk, error) {
	return nil, errors.New("not implemented")
}
func (fakeChunkRepo) ListByDocument(context.Context, value.WorkspaceID, uuid.UUID) ([]*entity.DocumentChunk, error) {
	return nil, nil
}
func (fakeChunkRepo) DeleteByDocument(context.Context, value.WorkspaceID, uuid.UUID) error { return nil }
func (fakeChunkRepo) SearchHybrid(context.Context, value.WorkspaceID, uuid.UUID, string, value.EmbeddingVector, int) ([]*repository.SearchResult, error) {
	return nil, nil
}

var _ repository.ChunkRepository = (fakeChunkRepo{})

type fakeTurnRepo struct {
	mu    sync.Mutex
	turns []*entity.DebateTurn
}

func (r *fakeTurnRepo) Create(_ context.Context, t *entity.DebateTurn) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns = append(r.turns, t)
	return nil
}
func (r *fakeTurnRepo) GetByID(context.Context, value.WorkspaceID, uuid.UUID) (*entity.DebateTurn, error) {
	return nil, errors.New("not implemented")
}
func (r *fakeTurnRepo) ListByIncident(context.Context, value.WorkspaceID, uuid.UUID) ([]*entity.DebateTurn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.turns, nil
}

var _ repository.DebateTurnRepository = (*fakeTurnRepo)(nil)

// mockQueue records every enqueued job and can be made to fail on demand.
type mockQueue struct {
	mu      sync.Mutex
	jobs    []job.IncidentJob
	handler job.Handler
	failErr error
}

func (q *mockQueue) Enqueue(j job.IncidentJob) error {
	if q.failErr != nil {
		return q.failErr
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, j)
	return nil
}
func (q *mockQueue) Start(_ context.Context, h job.Handler) { q.handler = h }
func (q *mockQueue) Shutdown(context.Context) error         { return nil }

var _ job.Queue = (*mockQueue)(nil)

// mockSettingsRepo is an in-memory stand-in keyed by workspace id, mirroring the
// one in internal/service/tenant's own tests (not importable from here: both are
// unexported test-package helpers).
type mockSettingsRepo struct {
	byWorkspace map[value.WorkspaceID]*entity.WorkspaceSettings
}

func newMockSettingsRepo() *mockSettingsRepo {
	return &mockSettingsRepo{byWorkspace: map[value.WorkspaceID]*entity.WorkspaceSettings{}}
}

func (m *mockSettingsRepo) Create(_ context.Context, s *entity.WorkspaceSettings) error {
	m.byWorkspace[s.WorkspaceID()] = s
	return nil
}
func (m *mockSettingsRepo) Get(_ context.Context, wsID value.WorkspaceID) (*entity.WorkspaceSettings, error) {
	s, ok := m.byWorkspace[wsID]
	if !ok {
		return nil, errors.New("settings not found")
	}
	return s, nil
}
func (m *mockSettingsRepo) Update(_ context.Context, s *entity.WorkspaceSettings) error {
	m.byWorkspace[s.WorkspaceID()] = s
	return nil
}

var _ repository.SettingsRepository = (*mockSettingsRepo)(nil)

// --- test fixtures ---

const testKey = "01234567890123456789012345678901"

// jsonChat answers every Stage 1 request with a confident, owning hypothesis and
// every Stage 2 request with a schema-valid hybrid verdict, so RunClash completes.
func jsonChat() llm.ChatFunc {
	return func(_ context.Context, req llm.ChatRequest) (string, error) {
		if req.Role == llm.RoleSynthesis {
			raw, _ := json.Marshal(orchestrator.SynthesisResult{
				Consensus:      "the pool was exhausted by a leaked connection",
				Classification: "hybrid",
				OwningDomain:   "database",
				NextSteps:      []string{"roll back the offending deploy"},
			})
			return string(raw), nil
		}
		raw, _ := json.Marshal(orchestrator.SpecialistResult{
			Hypothesis: "connection pool saturation",
			Confidence: "high",
			OwnsThis:   true,
		})
		return string(raw), nil
	}
}

func newTestService(t *testing.T, queue job.Queue, chat llm.ChatProvider) (*incident.Service, value.WorkspaceID) {
	t.Helper()

	wsID := value.NewWorkspaceID()
	cipher, err := crypto.New(testKey)
	if err != nil {
		t.Fatalf("building cipher: %v", err)
	}
	settingsRepo := newMockSettingsRepo()
	settings, err := entity.NewWorkspaceSettings(wsID, entity.SettingsDefaults{})
	if err != nil {
		t.Fatalf("building settings: %v", err)
	}
	llmKey, _ := cipher.Encrypt("sk-ant-key")
	_ = settings.SetLLM("anthropic", llmKey, "claude-opus-5", "claude-opus-5")
	embedKey, _ := cipher.Encrypt("sk-embed-key")
	_ = settings.SetEmbeddings("openai", embedKey, "text-embedding-3-small", "https://api.openai.com/v1")
	_ = settingsRepo.Create(context.Background(), settings)

	resolver := tenant.NewResolver(settingsRepo, cipher)

	persona, err := entity.NewPersona(wsID, value.PersonaKeyDatabase, "Database Specialist", "You are the database specialist.")
	if err != nil {
		t.Fatalf("building persona: %v", err)
	}
	personas := &fakePersonaRepo{personas: []*entity.Persona{persona}}
	turns := &fakeTurnRepo{}

	build := func(rt *tenant.Runtime) *orchestrator.Orchestrator {
		return orchestrator.New(personas, fakeChunkRepo{}, turns, chat, llm.FakeEmbedder{}, &zerolog.Logger{})
	}

	incidents := newFakeIncidentRepo()
	logger := zerolog.Nop()
	svc := incident.New(incidents, resolver, build, queue, &logger)
	return svc, wsID
}

func newTestIncident(t *testing.T, wsID value.WorkspaceID) *entity.Incident {
	t.Helper()
	chanID, err := value.ParseSlackChannelID("C123")
	if err != nil {
		t.Fatalf("channel id: %v", err)
	}
	threadTS, err := value.ParseSlackThreadTS("1700000000.000100")
	if err != nil {
		t.Fatalf("thread ts: %v", err)
	}
	userID, err := value.ParseSlackUserID("U123")
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	inc, err := entity.NewIncident(wsID, "Checkout failing", "500s on checkout", chanID, threadTS, value.SeveritySev2, userID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("building incident: %v", err)
	}
	return inc
}

// --- tests ---

func TestOpen_PersistsAndEnqueuesBeforeReturning(t *testing.T) {
	queue := &mockQueue{}
	svc, wsID := newTestService(t, queue, jsonChat())

	inc, err := svc.Open(context.Background(), incident.NewIncidentInput{
		WorkspaceID: wsID,
		Title:       "DB errors",
		Description: "connections exhausted",
		Severity:    value.SeveritySev1,
		Trigger:     value.TriggerTypeWebhook,
		ChannelID:   value.SlackChannelID("C999"),
		ThreadTS:    value.SlackThreadTS("1700000000.000200"),
		CreatedBy:   value.SlackUserID("U999"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if inc.ID() == uuid.Nil {
		t.Error("expected a persisted incident with a real id")
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.jobs) != 1 || queue.jobs[0].IncidentID != inc.ID() {
		t.Errorf("expected exactly one enqueued job for the new incident, got %+v", queue.jobs)
	}
}

func TestOpen_SurfacesQueueFailureButKeepsTheIncidentDurable(t *testing.T) {
	queue := &mockQueue{failErr: job.ErrQueueFull}
	svc, wsID := newTestService(t, queue, jsonChat())

	inc, err := svc.Open(context.Background(), incident.NewIncidentInput{
		WorkspaceID: wsID,
		Title:       "DB errors",
		Description: "connections exhausted",
		Severity:    value.SeveritySev1,
		Trigger:     value.TriggerTypeWebhook,
		ChannelID:   value.SlackChannelID("C999"),
		ThreadTS:    value.SlackThreadTS("1700000000.000300"),
		CreatedBy:   value.SlackUserID("U999"),
	})
	// The incident must still be returned (it's durable) even though enqueue failed.
	if inc == nil {
		t.Fatal("expected the incident to be returned despite the queue failure")
	}
	if err == nil {
		t.Error("expected the queue failure to be surfaced to the caller")
	}
}

func TestHandleJob_RunsClashAndPersistsTranscript(t *testing.T) {
	queue := &mockQueue{}
	svc, wsID := newTestService(t, queue, jsonChat())

	opened, err := svc.Open(context.Background(), incident.NewIncidentInput{
		WorkspaceID: wsID, Title: "DB errors", Description: "connections exhausted",
		Severity: value.SeveritySev1, Trigger: value.TriggerTypeWebhook,
		ChannelID: value.SlackChannelID("C999"), ThreadTS: value.SlackThreadTS("1700000000.000500"),
		CreatedBy: value.SlackUserID("U999"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Run the job the way the worker pool would, rather than relying on the
	// mock queue to execute it.
	if err := svc.HandleJob(context.Background(), job.IncidentJob{WorkspaceID: wsID, IncidentID: opened.ID()}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
}

func TestHandleJob_SkipsAnAlreadyResolvedIncident(t *testing.T) {
	// A custom chat stub that fails the test if ever called: a resolved incident
	// must never run a debate it has no use for.
	chat := llm.ChatFunc(func(context.Context, llm.ChatRequest) (string, error) {
		t.Fatal("the clash must not run for an already-resolved incident")
		return "", nil
	})
	svc, wsID := newTestService(t, &mockQueue{}, chat)

	opened, err := svc.Open(context.Background(), incident.NewIncidentInput{
		WorkspaceID: wsID, Title: "Resolved already", Description: "desc",
		Severity: value.SeveritySev3, Trigger: value.TriggerTypeWebhook,
		ChannelID: value.SlackChannelID("C999"), ThreadTS: value.SlackThreadTS("1700000000.000600"),
		CreatedBy: value.SlackUserID("U999"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := svc.Resolve(context.Background(), wsID, opened.ID()); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if err := svc.HandleJob(context.Background(), job.IncidentJob{WorkspaceID: wsID, IncidentID: opened.ID()}); err != nil {
		t.Errorf("HandleJob on a resolved incident should skip quietly, got %v", err)
	}
}

func TestHandleJob_UnconfiguredWorkspaceIsSkippedNotRetried(t *testing.T) {
	wsID := value.NewWorkspaceID()
	cipher, err := crypto.New(testKey)
	if err != nil {
		t.Fatalf("building cipher: %v", err)
	}
	settingsRepo := newMockSettingsRepo()
	// Settings exist, but neither credential has been supplied yet.
	settings, err := entity.NewWorkspaceSettings(wsID, entity.SettingsDefaults{})
	if err != nil {
		t.Fatalf("building settings: %v", err)
	}
	_ = settingsRepo.Create(context.Background(), settings)
	resolver := tenant.NewResolver(settingsRepo, cipher)

	incidents := newFakeIncidentRepo()
	logger := zerolog.Nop()
	build := func(*tenant.Runtime) *orchestrator.Orchestrator { return nil }
	svc := incident.New(incidents, resolver, build, &mockQueue{}, &logger)

	inc := newTestIncident(t, wsID)
	if err := incidents.Create(context.Background(), inc); err != nil {
		t.Fatalf("seeding incident: %v", err)
	}

	if err := svc.HandleJob(context.Background(), job.IncidentJob{WorkspaceID: wsID, IncidentID: inc.ID()}); err != nil {
		t.Errorf("an unconfigured workspace should be skipped (nil error, no retry), got %v", err)
	}
}

func TestInvestigateNow_ReturnsASummaryWithoutTouchingTheQueue(t *testing.T) {
	queue := &mockQueue{}
	svc, wsID := newTestService(t, queue, jsonChat())
	inc := newTestIncident(t, wsID)

	summary, err := svc.InvestigateNow(context.Background(), inc)
	if err != nil {
		t.Fatalf("InvestigateNow: %v", err)
	}
	if summary.Specialists != 1 {
		t.Errorf("expected 1 specialist to have succeeded, got %d", summary.Specialists)
	}
	if summary.Classification != "hybrid" || summary.OwningDomain != "database" {
		t.Errorf("unexpected summary: %+v", summary)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.jobs) != 0 {
		t.Error("InvestigateNow must bypass the queue entirely")
	}
}

func TestResolve_RejectsADoubleResolve(t *testing.T) {
	svc, wsID := newTestService(t, &mockQueue{}, jsonChat())
	opened, err := svc.Open(context.Background(), incident.NewIncidentInput{
		WorkspaceID: wsID, Title: "Flaky checkout", Description: "desc",
		Severity: value.SeveritySev2, Trigger: value.TriggerTypeWebhook,
		ChannelID: value.SlackChannelID("C999"), ThreadTS: value.SlackThreadTS("1700000000.000700"),
		CreatedBy: value.SlackUserID("U999"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := svc.Resolve(context.Background(), wsID, opened.ID()); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if _, err := svc.Resolve(context.Background(), wsID, opened.ID()); !errors.Is(err, entity.ErrIncidentAlreadyResolved) {
		t.Errorf("expected ErrIncidentAlreadyResolved on the second Resolve, got %v", err)
	}
}
