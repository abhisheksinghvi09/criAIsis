package handler_test

import (
	"context"
	"errors"
	"sync"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// noopTxManager runs the callback directly against the caller's context. The
// mock repositories in this file aren't transaction-aware, so there is nothing
// for a real transaction boundary to coordinate in these tests.
type noopTxManager struct{}

func (noopTxManager) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type mockIncidentRepo struct {
	mu        sync.RWMutex
	incidents map[uuid.UUID]*entity.Incident
}

func newMockIncidentRepo() *mockIncidentRepo {
	return &mockIncidentRepo{incidents: make(map[uuid.UUID]*entity.Incident)}
}

func (m *mockIncidentRepo) Create(ctx context.Context, inc *entity.Incident) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incidents[inc.ID()] = inc
	return nil
}

func (m *mockIncidentRepo) GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inc, ok := m.incidents[id]
	if !ok || inc.WorkspaceID() != wsID {
		return nil, errors.New("not found")
	}
	return inc, nil
}

func (m *mockIncidentRepo) GetBySlackThread(ctx context.Context, wsID value.WorkspaceID, channelID value.SlackChannelID, threadTS value.SlackThreadTS) (*entity.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, inc := range m.incidents {
		if inc.WorkspaceID() == wsID && inc.SlackChannelID() == channelID && inc.SlackThreadTS() == threadTS {
			return inc, nil
		}
	}
	return nil, nil
}

func (m *mockIncidentRepo) ListActive(ctx context.Context, wsID value.WorkspaceID, limit, offset int) ([]*entity.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*entity.Incident
	for _, inc := range m.incidents {
		if inc.WorkspaceID() == wsID && !inc.Status().IsResolved() {
			list = append(list, inc)
		}
	}
	return list, nil
}

func (m *mockIncidentRepo) ListRecent(ctx context.Context, wsID value.WorkspaceID, limit, offset int) ([]*entity.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*entity.Incident
	for _, inc := range m.incidents {
		if inc.WorkspaceID() == wsID {
			list = append(list, inc)
		}
	}
	return list, nil
}

func (m *mockIncidentRepo) Update(ctx context.Context, inc *entity.Incident) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incidents[inc.ID()] = inc
	return nil
}

type mockReproRepo struct {
	mu     sync.RWMutex
	repros map[uuid.UUID]*entity.SandboxReproduction
}

func newMockReproRepo() *mockReproRepo {
	return &mockReproRepo{repros: make(map[uuid.UUID]*entity.SandboxReproduction)}
}

func (m *mockReproRepo) Create(ctx context.Context, sr *entity.SandboxReproduction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repros[sr.ID()] = sr
	return nil
}

func (m *mockReproRepo) GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.SandboxReproduction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sr, ok := m.repros[id]
	if !ok || sr.WorkspaceID() != wsID {
		return nil, errors.New("not found")
	}
	return sr, nil
}

func (m *mockReproRepo) GetActiveForIncident(ctx context.Context, wsID value.WorkspaceID, incidentID uuid.UUID) (*entity.SandboxReproduction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sr := range m.repros {
		if sr.WorkspaceID() == wsID && sr.IncidentID() == incidentID && (sr.Status() == value.SandboxStatusProvisioning || sr.Status() == value.SandboxStatusReady) {
			return sr, nil
		}
	}
	return nil, nil
}

func (m *mockReproRepo) Update(ctx context.Context, sr *entity.SandboxReproduction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repros[sr.ID()] = sr
	return nil
}

type mockProvisioner struct {
	fail  bool
	block chan struct{}
}

func (p *mockProvisioner) Provision(ctx context.Context, scenarioID string) (string, error) {
	if p.block != nil {
		select {
		case <-p.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if p.fail {
		return "", errors.New("docker daemon failure")
	}
	return "docker://container-" + scenarioID, nil
}
