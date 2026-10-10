package repository

import (
	"context"

	"criaisis/internal/domain/entity"

	"github.com/google/uuid"
)

// SandboxReproductionRepository defines persistence operations for sandbox reproduction runs.
type SandboxReproductionRepository interface {
	// Create persists a newly triggered reproduction run in Provisioning status.
	Create(ctx context.Context, sr *entity.SandboxReproduction) error

	// GetByID retrieves a reproduction attempt by its primary UUID.
	GetByID(ctx context.Context, id uuid.UUID) (*entity.SandboxReproduction, error)

	// GetActiveForIncident retrieves any currently active (Provisioning or Ready) reproduction for an incident.
	GetActiveForIncident(ctx context.Context, incidentID uuid.UUID) (*entity.SandboxReproduction, error)

	// Update persists status changes, container references, or mapped scenario IDs.
	Update(ctx context.Context, sr *entity.SandboxReproduction) error
}
