package orchestrator_test

import (
	"context"
	"errors"
	"sync"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// memPersonas is an in-memory PersonaRepository for clash tests.
type memPersonas struct{ personas []*entity.Persona }

func (m *memPersonas) Create(context.Context, *entity.Persona) error { return nil }

func (m *memPersonas) GetByID(_ context.Context, _ value.WorkspaceID, id uuid.UUID) (*entity.Persona, error) {
	for _, p := range m.personas {
		if p.ID() == id {
			return p, nil
		}
	}
	return nil, errors.New("persona not found")
}

func (m *memPersonas) GetByKey(_ context.Context, _ value.WorkspaceID, key value.PersonaKey) (*entity.Persona, error) {
	for _, p := range m.personas {
		if p.Key() == key {
			return p, nil
		}
	}
	return nil, errors.New("persona not found")
}

func (m *memPersonas) ListByWorkspace(context.Context, value.WorkspaceID) ([]*entity.Persona, error) {
	return m.personas, nil
}

func (m *memPersonas) Update(context.Context, *entity.Persona) error { return nil }

// memChunks is an in-memory ChunkRepository. SearchHybrid returns whatever chunks
// were registered for the requested persona, which is enough to verify that the
// orchestrator scopes retrieval per persona.
type memChunks struct {
	byPersona map[uuid.UUID][]*repository.SearchResult
	failFor   map[uuid.UUID]bool
}

func (m *memChunks) BatchCreate(context.Context, []*entity.DocumentChunk) error { return nil }

func (m *memChunks) GetByID(context.Context, value.WorkspaceID, uuid.UUID) (*entity.DocumentChunk, error) {
	return nil, errors.New("not implemented")
}

func (m *memChunks) ListByDocument(context.Context, value.WorkspaceID, uuid.UUID) ([]*entity.DocumentChunk, error) {
	return nil, nil
}

func (m *memChunks) DeleteByDocument(context.Context, value.WorkspaceID, uuid.UUID) error { return nil }

func (m *memChunks) SearchHybrid(
	_ context.Context,
	_ value.WorkspaceID,
	personaID uuid.UUID,
	_ string,
	_ value.EmbeddingVector,
	_ int,
) ([]*repository.SearchResult, error) {
	if m.failFor[personaID] {
		return nil, errors.New("retrieval backend unavailable")
	}
	return m.byPersona[personaID], nil
}

// memTurns is an append-only in-memory DebateTurnRepository.
//
// Stage 1 writes from four goroutines at once, so this stands in for a repository
// that must be safe for concurrent use; the pgx pool provides that in production.
type memTurns struct {
	mu    sync.Mutex
	turns []*entity.DebateTurn
}

func (m *memTurns) Create(_ context.Context, turn *entity.DebateTurn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns = append(m.turns, turn)
	return nil
}

func (m *memTurns) GetByID(_ context.Context, _ value.WorkspaceID, id uuid.UUID) (*entity.DebateTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.turns {
		if t.ID() == id {
			return t, nil
		}
	}
	return nil, errors.New("turn not found")
}

func (m *memTurns) ListByIncident(_ context.Context, _ value.WorkspaceID, incidentID uuid.UUID) ([]*entity.DebateTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*entity.DebateTurn
	for _, t := range m.turns {
		if t.IncidentID() == incidentID {
			out = append(out, t)
		}
	}
	return out, nil
}

// all returns a snapshot of every persisted turn.
func (m *memTurns) all() []*entity.DebateTurn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*entity.DebateTurn(nil), m.turns...)
}

// stageCount reports how many persisted turns belong to a stage.
func (m *memTurns) stageCount(stage value.Stage) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int
	for _, t := range m.turns {
		if t.Stage() == stage {
			n++
		}
	}
	return n
}

var (
	_ repository.PersonaRepository    = (*memPersonas)(nil)
	_ repository.ChunkRepository      = (*memChunks)(nil)
	_ repository.DebateTurnRepository = (*memTurns)(nil)
)
