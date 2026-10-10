package repository

import (
	"context"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// SandboxReproductionRepository defines persistence operations for sandbox reproduction runs.
// Every lookup takes the caller's workspace so cross-tenant access fails in SQL,
// the same way every other repository in this package is scoped.
type SandboxReproductionRepository interface {
	// Create persists a newly triggered reproduction run in Provisioning status.
	Create(ctx context.Context, sr *entity.SandboxReproduction) error

	// GetByID retrieves a reproduction attempt by its primary UUID, scoped to wsID.
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.SandboxReproduction, error)

	// GetActiveForIncident retrieves any currently active (Provisioning or Ready) reproduction for an incident, scoped to wsID.
	GetActiveForIncident(ctx context.Context, wsID value.WorkspaceID, incidentID uuid.UUID) (*entity.SandboxReproduction, error)

	// Update persists status changes, container references, or mapped scenario IDs, scoped to sr's workspace.
	Update(ctx context.Context, sr *entity.SandboxReproduction) error
}
