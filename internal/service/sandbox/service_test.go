package sandbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
	"criaisis/internal/service/sandbox"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

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
	return nil, nil
}

func (m *mockIncidentRepo) ListActive(ctx context.Context, wsID value.WorkspaceID, limit, offset int) ([]*entity.Incident, error) {
	return nil, nil
}

func (m *mockIncidentRepo) ListRecent(ctx context.Context, wsID value.WorkspaceID, limit, offset int) ([]*entity.Incident, error) {
	return nil, nil
}

func (m *mockIncidentRepo) Update(ctx context.Context, inc *entity.Incident) error {
	m.incidents[inc.ID()] = inc
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

func TestSandboxService(t *testing.T) {
	logger := zerolog.Nop()
	wsID := value.NewWorkspaceID()
	incRepo := newMockIncidentRepo()
	reproRepo := newMockReproRepo()
	prov := &mockProvisioner{block: make(chan struct{})}
	svc := sandbox.NewService(reproRepo, incRepo, prov, &logger)

	chanID, _ := value.ParseSlackChannelID("C123")
	threadTS, _ := value.ParseSlackThreadTS("123.456")
	userID, _ := value.ParseSlackUserID("U123")

	// Create an investigating incident
	activeInc, _ := entity.NewIncident(wsID, "Active Incident", "desc", chanID, threadTS, value.SeveritySev3, userID, nil)
	_ = incRepo.Create(context.Background(), activeInc)

	// Create a resolved incident
	resolvedInc, _ := entity.NewIncident(wsID, "Resolved Incident", "desc", chanID, threadTS, value.SeveritySev3, userID, nil)
	_ = resolvedInc.Resolve(time.Now().UTC())
	_ = resolvedInc.SetIncidentContext(value.NewIncidentContext(
		"api",
		"",
		[]string{"container terminated with 137 OOMKilled", "memory limit exceeded"},
		[]string{"runtime.makeslice"},
		nil,
	))
	_ = incRepo.Create(context.Background(), resolvedInc)

	t.Run("BR5.2: reject reproduction on active incident", func(t *testing.T) {
		_, err := svc.Trigger(context.Background(), wsID, activeInc.ID())
		if !errors.Is(err, sandbox.ErrIncidentNotResolved) {
			t.Fatalf("expected ErrIncidentNotResolved, got %v", err)
		}
	})

	t.Run("BR5.1 and BR5.3: trigger on resolved incident matches scenario and provisions", func(t *testing.T) {
		sr, err := svc.Trigger(context.Background(), wsID, resolvedInc.ID())
		if err != nil {
			t.Fatalf("trigger failed: %v", err)
		}

		if sr.ScenarioID() != "oom_crashloop" {
			t.Errorf("expected scenario oom_crashloop, got %s", sr.ScenarioID())
		}

		// BR5.1: second concurrent trigger fails
		_, err = svc.Trigger(context.Background(), wsID, resolvedInc.ID())
		if !errors.Is(err, sandbox.ErrActiveReproductionExists) {
			t.Fatalf("expected ErrActiveReproductionExists, got %v", err)
		}
	})

	t.Run("BR5.6: access endpoint check", func(t *testing.T) {
		sr, err := reproRepo.GetActiveForIncident(context.Background(), wsID, resolvedInc.ID())
		if err != nil || sr == nil {
			t.Fatal("expected active reproduction")
		}

		// Not ready yet -> ErrSandboxNotReady
		_, err = svc.GetAccess(context.Background(), wsID, sr.ID())
		if !errors.Is(err, sandbox.ErrSandboxNotReady) {
			t.Fatalf("expected ErrSandboxNotReady, got %v", err)
		}

		// Unblock provisioning and wait for Ready status
		close(prov.block)
		for i := 0; i < 30; i++ {
			time.Sleep(10 * time.Millisecond)
			if s, _ := reproRepo.GetByID(context.Background(), wsID, sr.ID()); s.Status() == value.SandboxStatusReady {
				break
			}
		}

		access, err := svc.GetAccess(context.Background(), wsID, sr.ID())
		if err != nil {
			t.Fatalf("expected access, got %v", err)
		}
		if access.ContainerRef != "docker://container-oom_crashloop" {
			t.Errorf("got %s, want docker://container-oom_crashloop", access.ContainerRef)
		}
		if access.AccessURL == "" || access.LogsURL == "" {
			t.Errorf("expected non-empty access and logs URLs")
		}
	})

	t.Run("BR5.4: select scenario from unmatched fallback", func(t *testing.T) {
		// New resolved incident with no matching logs
		unmatchedInc, _ := entity.NewIncident(wsID, "Generic Fault", "desc", chanID, threadTS, value.SeveritySev3, userID, nil)
		_ = unmatchedInc.Resolve(time.Now().UTC())
		_ = incRepo.Create(context.Background(), unmatchedInc)

		sr, err := svc.Trigger(context.Background(), wsID, unmatchedInc.ID())
		if err != nil {
			t.Fatalf("trigger failed: %v", err)
		}
		if sr.Status() != value.SandboxStatusUnmatchedFallback {
			t.Fatalf("expected status UnmatchedFallback, got %s", sr.Status())
		}

		// Select valid scenario
		updated, err := svc.SelectScenario(context.Background(), wsID, sr.ID(), "db_connection_exhaustion")
		if err != nil {
			t.Fatalf("select scenario failed: %v", err)
		}
		if updated.ScenarioID() != "db_connection_exhaustion" {
			t.Errorf("got %s, want db_connection_exhaustion", updated.ScenarioID())
		}
		if updated.Status() != value.SandboxStatusProvisioning {
			t.Errorf("expected status Provisioning, got %s", updated.Status())
		}
	})
}
