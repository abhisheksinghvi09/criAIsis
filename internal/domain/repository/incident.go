package repository

import (
	"context"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// IncidentRepository defines the persistence contract for active war rooms.
type IncidentRepository interface {
	// Create persists a new incident initiated from a Slack slash command or webhook.
	Create(ctx context.Context, inc *entity.Incident) error

	// GetByID retrieves an incident by its internal UUID within a tenant.
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Incident, error)

	// GetBySlackThread finds an active incident mapped to a specific Slack channel and thread timestamp.
	GetBySlackThread(ctx context.Context, wsID value.WorkspaceID, channelID value.SlackChannelID, threadTS value.SlackThreadTS) (*entity.Incident, error)

	// ListActive returns currently investigating incidents for the dashboard, filtered by status.
	ListActive(ctx context.Context, wsID value.WorkspaceID, limit, offset int) ([]*entity.Incident, error)

	// ListRecent returns chronological incidents across all statuses for audit history.
	ListRecent(ctx context.Context, wsID value.WorkspaceID, limit, offset int) ([]*entity.Incident, error)

	// Update persists modifications to an incident (e.g. resolution timestamp, severity elevation).
	Update(ctx context.Context, inc *entity.Incident) error
}

// DebateTurnRepository defines the append-only persistence contract for war room debate messages.
// Intentionally omits Update and Delete methods to protect post-mortem audit durability.
type DebateTurnRepository interface {
	// Create appends a specialist hypothesis, consensus synthesis, or follow-up response turn.
	Create(ctx context.Context, turn *entity.DebateTurn) error

	// GetByID retrieves a single debate turn by primary key.
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.DebateTurn, error)

	// ListByIncident loads the complete, chronological debate transcript for rendering in Slack and the dashboard.
	ListByIncident(ctx context.Context, wsID value.WorkspaceID, incidentID uuid.UUID) ([]*entity.DebateTurn, error)
}
