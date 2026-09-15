package repository

import (
	"context"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// PersonaRepository defines the persistence contract for domain specialist personas.
// Operations are strictly scoped to the requesting workspace.
type PersonaRepository interface {
	// Create persists a new specialist persona during workspace initialization.
	Create(ctx context.Context, persona *entity.Persona) error

	// GetByID retrieves a persona by primary key within a tenant.
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Persona, error)

	// GetByKey retrieves one of the 4 specialist domains (network, database, application, security).
	GetByKey(ctx context.Context, wsID value.WorkspaceID, key value.PersonaKey) (*entity.Persona, error)

	// ListByWorkspace returns all specialist personas configured for a tenant.
	ListByWorkspace(ctx context.Context, wsID value.WorkspaceID) ([]*entity.Persona, error)

	// Update persists prompt edits or enable/disable toggle changes.
	Update(ctx context.Context, persona *entity.Persona) error
}
