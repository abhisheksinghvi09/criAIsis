package repository

import (
	"context"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
)

// WorkspaceRepository defines the persistence contract for tenant accounts.
// All lookups outside Slack OAuth must be scoped to a validated WorkspaceID.
type WorkspaceRepository interface {
	// Create persists a newly installed Slack workspace and its encrypted bot token.
	Create(ctx context.Context, ws *entity.Workspace) error

	// GetByID retrieves a workspace by its internal UUID.
	GetByID(ctx context.Context, id value.WorkspaceID) (*entity.Workspace, error)

	// GetBySlackTeamID resolves an incoming Slack webhook (e.g. slash command) to its internal tenant.
	GetBySlackTeamID(ctx context.Context, teamID string) (*entity.Workspace, error)

	// Update persists modifications to workspace attributes (e.g. name change, token rotation).
	Update(ctx context.Context, ws *entity.Workspace) error
}
