package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresWorkspaceRepository struct {
	pool *pgxpool.Pool
}

var _ repository.WorkspaceRepository = (*PostgresWorkspaceRepository)(nil)

func NewWorkspaceRepository(pool *pgxpool.Pool) *PostgresWorkspaceRepository {
	return &PostgresWorkspaceRepository{pool: pool}
}

func (r *PostgresWorkspaceRepository) Create(ctx context.Context, ws *entity.Workspace) error {
	const query = `
		INSERT INTO workspaces (id, slack_team_id, slack_team_name, slack_bot_token_encrypted, webhook_secret_hash, admin_api_key_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
	`
	exec := GetExecutor(ctx, r.pool)
	_, err := exec.Exec(ctx, query,
		ws.ID().UUID(),
		ws.SlackTeamID(),
		ws.SlackTeamName(),
		ws.SlackBotTokenEncrypted(),
		ws.WebhookSecretHash(),
		ws.AdminAPIKeyHash(),
		ws.CreatedAt(),
		ws.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("inserting workspace: %w", err)
	}
	return nil
}

func (r *PostgresWorkspaceRepository) GetByID(ctx context.Context, id value.WorkspaceID) (*entity.Workspace, error) {
	const query = `
		SELECT id, slack_team_id, slack_team_name, slack_bot_token_encrypted, webhook_secret_hash, admin_api_key_hash, created_at, updated_at
		FROM workspaces
		WHERE id = $1;
	`
	exec := GetExecutor(ctx, r.pool)
	var (
		rawID      uuid.UUID
		teamID     string
		teamName   string
		tokenBytes []byte
		hookHash   []byte
		adminHash  []byte
		createdAt  time.Time
		updatedAt  time.Time
	)

	err := exec.QueryRow(ctx, query, id.UUID()).Scan(
		&rawID,
		&teamID,
		&teamName,
		&tokenBytes,
		&hookHash,
		&adminHash,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("workspace with id '%s' not found", id)
		}
		return nil, fmt.Errorf("querying workspace by id: %w", err)
	}

	wsID, err := value.FromUUID(rawID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in db: %w", err)
	}

	return entity.ReconstituteWorkspace(wsID, teamID, teamName, tokenBytes, hookHash, adminHash, createdAt, updatedAt)
}

func (r *PostgresWorkspaceRepository) GetBySlackTeamID(ctx context.Context, teamID string) (*entity.Workspace, error) {
	const query = `
		SELECT id, slack_team_id, slack_team_name, slack_bot_token_encrypted, webhook_secret_hash, admin_api_key_hash, created_at, updated_at
		FROM workspaces
		WHERE slack_team_id = $1;
	`
	exec := GetExecutor(ctx, r.pool)
	var (
		rawID      uuid.UUID
		rawTeamID  string
		teamName   string
		tokenBytes []byte
		hookHash   []byte
		adminHash  []byte
		createdAt  time.Time
		updatedAt  time.Time
	)

	err := exec.QueryRow(ctx, query, teamID).Scan(
		&rawID,
		&rawTeamID,
		&teamName,
		&tokenBytes,
		&hookHash,
		&adminHash,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("workspace with slack team id '%s' not found", teamID)
		}
		return nil, fmt.Errorf("querying workspace by slack team id: %w", err)
	}

	wsID, err := value.FromUUID(rawID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in db: %w", err)
	}

	return entity.ReconstituteWorkspace(wsID, rawTeamID, teamName, tokenBytes, hookHash, adminHash, createdAt, updatedAt)
}

func (r *PostgresWorkspaceRepository) Update(ctx context.Context, ws *entity.Workspace) error {
	const query = `
		UPDATE workspaces
		SET slack_team_name = $2,
		    slack_bot_token_encrypted = $3,
		    webhook_secret_hash = $4,
		    admin_api_key_hash = $5,
		    updated_at = $6
		WHERE id = $1;
	`
	exec := GetExecutor(ctx, r.pool)
	tag, err := exec.Exec(ctx, query,
		ws.ID().UUID(),
		ws.SlackTeamName(),
		ws.SlackBotTokenEncrypted(),
		ws.WebhookSecretHash(),
		ws.AdminAPIKeyHash(),
		ws.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("updating workspace: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("workspace '%s' not found for update", ws.ID())
	}
	return nil
}
