package handler

import (
	"context"
	"fmt"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
)

// Provisioner creates a fully usable tenant. Both ways in (the platform API and
// the Slack install) go through it, so neither can produce a workspace the clash
// engine cannot run for.
type Provisioner struct {
	workspaces repository.WorkspaceRepository
	settings   repository.SettingsRepository
	personas   repository.PersonaRepository
	tx         repository.TransactionManager
	defaults   entity.SettingsDefaults
}

// NewProvisioner wires the provisioner.
func NewProvisioner(
	workspaces repository.WorkspaceRepository,
	settings repository.SettingsRepository,
	personas repository.PersonaRepository,
	tx repository.TransactionManager,
	defaults entity.SettingsDefaults,
) *Provisioner {
	return &Provisioner{workspaces: workspaces, settings: settings, personas: personas, tx: tx, defaults: defaults}
}

// Credentials are shown to the tenant once; only their digests are stored.
type Credentials struct {
	AdminKey   string
	AlertToken string
}

// Provision generates the workspace's credentials and writes the workspace, its
// settings row and its default specialists atomically.
func (p *Provisioner) Provision(ctx context.Context, ws *entity.Workspace) (Credentials, error) {
	adminKey, err := ws.GenerateAdminAPIKey()
	if err != nil {
		return Credentials{}, fmt.Errorf("generating admin key: %w", err)
	}
	alertToken, err := ws.GenerateWebhookSecret()
	if err != nil {
		return Credentials{}, fmt.Errorf("generating alert token: %w", err)
	}
	settings, err := entity.NewWorkspaceSettings(ws.ID(), p.defaults)
	if err != nil {
		return Credentials{}, fmt.Errorf("preparing settings: %w", err)
	}
	personas, err := entity.DefaultPersonas(ws.ID())
	if err != nil {
		return Credentials{}, fmt.Errorf("building specialists: %w", err)
	}

	// A workspace is useless without its settings row and default personas, so a
	// failure partway through must not leave one half-provisioned.
	err = p.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := p.workspaces.Create(ctx, ws); err != nil {
			return fmt.Errorf("creating workspace: %w", err)
		}
		if err := p.settings.Create(ctx, settings); err != nil {
			return fmt.Errorf("creating settings: %w", err)
		}
		for _, persona := range personas {
			if err := p.personas.Create(ctx, persona); err != nil {
				return fmt.Errorf("seeding specialists: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{AdminKey: adminKey, AlertToken: alertToken}, nil
}
