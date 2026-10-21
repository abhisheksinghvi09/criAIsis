package repository

import (
	"context"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
)

// SettingsRepository persists one tenant's own integration credentials.
//
// Every method is scoped by WorkspaceID: there is deliberately no "list all
// settings" operation, because no request in this product legitimately needs to
// read more than one tenant's credentials.
type SettingsRepository interface {
	// Create stores the initial, unconfigured settings for a new workspace.
	Create(ctx context.Context, settings *entity.WorkspaceSettings) error

	// Get retrieves a tenant's settings.
	Get(ctx context.Context, wsID value.WorkspaceID) (*entity.WorkspaceSettings, error)

	// Update persists credential or destination changes.
	Update(ctx context.Context, settings *entity.WorkspaceSettings) error
}
