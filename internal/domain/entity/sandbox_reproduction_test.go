package entity_test

import (
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

func TestNewSandboxReproduction(t *testing.T) {
	wsID := value.NewWorkspaceID()
	incID := uuid.New()

	t.Run("valid creation", func(t *testing.T) {
		sr, err := entity.NewSandboxReproduction(incID, wsID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sr.ID() == uuid.Nil {
			t.Error("expected non-nil ID")
		}
		if sr.IncidentID() != incID {
			t.Errorf("got %v, want %v", sr.IncidentID(), incID)
		}
		if sr.WorkspaceID() != wsID {
			t.Errorf("got %v, want %v", sr.WorkspaceID(), wsID)
		}
		if sr.Status() != value.SandboxStatusProvisioning {
			t.Errorf("got status %v, want %v", sr.Status(), value.SandboxStatusProvisioning)
		}
	})

	t.Run("nil incident ID fails", func(t *testing.T) {
		_, err := entity.NewSandboxReproduction(uuid.Nil, wsID)
		if err == nil {
			t.Error("expected error for nil incident ID")
		}
	})

	t.Run("zero workspace ID fails", func(t *testing.T) {
		_, err := entity.NewSandboxReproduction(incID, value.WorkspaceID{})
		if err == nil {
			t.Error("expected error for zero workspace ID")
		}
	})
}

func TestSandboxReproduction_StateTransitions(t *testing.T) {
	wsID := value.NewWorkspaceID()
	incID := uuid.New()
	sr, _ := entity.NewSandboxReproduction(incID, wsID)

	t.Run("mark ready", func(t *testing.T) {
		sr.MarkReady("docker://container-123")
		if sr.Status() != value.SandboxStatusReady {
			t.Errorf("got status %v, want %v", sr.Status(), value.SandboxStatusReady)
		}
		if sr.ContainerRef() != "docker://container-123" {
			t.Errorf("got ref %v, want %v", sr.ContainerRef(), "docker://container-123")
		}
		if sr.ReadyAt() == nil {
			t.Error("expected readyAt to be set")
		}
	})

	t.Run("mark failed", func(t *testing.T) {
		sr.MarkFailed()
		if sr.Status() != value.SandboxStatusFailed {
			t.Errorf("got status %v, want %v", sr.Status(), value.SandboxStatusFailed)
		}
	})

	t.Run("unmatched fallback and select scenario", func(t *testing.T) {
		sr.SetUnmatchedFallback()
		if sr.Status() != value.SandboxStatusUnmatchedFallback {
			t.Errorf("got status %v, want %v", sr.Status(), value.SandboxStatusUnmatchedFallback)
		}

		// select scenario from unmatched succeeds
		err := sr.SelectScenario("oom_crashloop")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sr.Status() != value.SandboxStatusProvisioning {
			t.Errorf("got status %v, want %v", sr.Status(), value.SandboxStatusProvisioning)
		}
		if sr.ScenarioID() != "oom_crashloop" {
			t.Errorf("got scenario %v, want oom_crashloop", sr.ScenarioID())
		}

		// selecting scenario when not in unmatched fails
		err = sr.SelectScenario("slow_leak")
		if err != entity.ErrSandboxNotUnmatched {
			t.Errorf("expected ErrSandboxNotUnmatched, got %v", err)
		}
	})
}
