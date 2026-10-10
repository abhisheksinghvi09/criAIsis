package entity

import (
	"errors"
	"strings"
	"sync"
	"time"

	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

var (
	ErrSandboxNotUnmatched = errors.New("sandbox reproduction is not in UnmatchedFallback status")
	ErrInvalidScenarioID   = errors.New("invalid or empty scenario ID")
)

// SandboxReproduction tracks an attempt to reproduce an incident in an isolated sandbox.
type SandboxReproduction struct {
	mu           sync.RWMutex
	id           uuid.UUID
	incidentID   uuid.UUID
	workspaceID  value.WorkspaceID
	status       value.SandboxStatus
	scenarioID   string
	containerRef string
	createdAt    time.Time
	readyAt      *time.Time
	updatedAt    time.Time
}

// NewSandboxReproduction initializes a reproduction attempt in Provisioning status.
func NewSandboxReproduction(incidentID uuid.UUID, workspaceID value.WorkspaceID) (*SandboxReproduction, error) {
	if incidentID == uuid.Nil {
		return nil, errors.New("incident ID cannot be nil")
	}
	if workspaceID.IsZero() {
		return nil, errors.New("workspace ID cannot be zero")
	}
	now := time.Now().UTC()
	return &SandboxReproduction{
		id:          uuid.New(),
		incidentID:  incidentID,
		workspaceID: workspaceID,
		status:      value.SandboxStatusProvisioning,
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

// ReconstituteSandboxReproduction rebuilds an entity from persistent storage.
func ReconstituteSandboxReproduction(
	id uuid.UUID,
	incidentID uuid.UUID,
	workspaceID value.WorkspaceID,
	status value.SandboxStatus,
	scenarioID string,
	containerRef string,
	createdAt time.Time,
	readyAt *time.Time,
	updatedAt time.Time,
) *SandboxReproduction {
	return &SandboxReproduction{
		id:           id,
		incidentID:   incidentID,
		workspaceID:  workspaceID,
		status:       status,
		scenarioID:   scenarioID,
		containerRef: containerRef,
		createdAt:    createdAt,
		readyAt:      readyAt,
		updatedAt:    updatedAt,
	}
}

func (s *SandboxReproduction) ID() uuid.UUID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

func (s *SandboxReproduction) IncidentID() uuid.UUID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.incidentID
}

func (s *SandboxReproduction) WorkspaceID() value.WorkspaceID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workspaceID
}

func (s *SandboxReproduction) Status() value.SandboxStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *SandboxReproduction) ScenarioID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scenarioID
}

func (s *SandboxReproduction) ContainerRef() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.containerRef
}

func (s *SandboxReproduction) CreatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.createdAt
}

func (s *SandboxReproduction) ReadyAt() *time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readyAt
}

func (s *SandboxReproduction) UpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

// SetScenarioID assigns the mapped scenario to the reproduction.
func (s *SandboxReproduction) SetScenarioID(scenarioID string) error {
	trimmed := strings.TrimSpace(scenarioID)
	if trimmed == "" {
		return ErrInvalidScenarioID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scenarioID = trimmed
	s.updatedAt = time.Now().UTC()
	return nil
}

// MarkReady transitions the status to Ready and sets container handle and completion time.
func (s *SandboxReproduction) MarkReady(containerRef string) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = value.SandboxStatusReady
	s.containerRef = strings.TrimSpace(containerRef)
	s.readyAt = &now
	s.updatedAt = now
}

// MarkFailed records provisioning failure.
func (s *SandboxReproduction) MarkFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = value.SandboxStatusFailed
	s.updatedAt = time.Now().UTC()
}

// SetUnmatchedFallback transitions to UnmatchedFallback when automatic mapping cannot resolve a scenario.
func (s *SandboxReproduction) SetUnmatchedFallback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = value.SandboxStatusUnmatchedFallback
	s.updatedAt = time.Now().UTC()
}

// SelectScenario sets the manual scenario choice and restarts provisioning.
func (s *SandboxReproduction) SelectScenario(scenarioID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != value.SandboxStatusUnmatchedFallback {
		return ErrSandboxNotUnmatched
	}
	trimmed := strings.TrimSpace(scenarioID)
	if trimmed == "" {
		return ErrInvalidScenarioID
	}
	s.scenarioID = trimmed
	s.status = value.SandboxStatusProvisioning
	s.updatedAt = time.Now().UTC()
	return nil
}
