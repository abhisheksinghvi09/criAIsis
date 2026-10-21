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
		SELECT id, slack_team_id, slack_team_name, slack_bot_token_encrypted, webhook_secret_hash, admin_api_key_hash, created_at, updated_at, version
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
		version    int64
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
		&version,
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

	ws, err := entity.ReconstituteWorkspace(wsID, teamID, teamName, tokenBytes, hookHash, adminHash, createdAt, updatedAt)
	if err != nil {
		return nil, err
	}
	return ws.WithVersion(version), nil
}

func (r *PostgresWorkspaceRepository) GetBySlackTeamID(ctx context.Context, teamID string) (*entity.Workspace, error) {
	const query = `
		SELECT id, slack_team_id, slack_team_name, slack_bot_token_encrypted, webhook_secret_hash, admin_api_key_hash, created_at, updated_at, version
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
		version    int64
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
		&version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("slack team id '%s': %w", teamID, repository.ErrWorkspaceNotFound)
		}
		return nil, fmt.Errorf("querying workspace by slack team id: %w", err)
	}

	wsID, err := value.FromUUID(rawID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in db: %w", err)
	}

	ws, err := entity.ReconstituteWorkspace(wsID, rawTeamID, teamName, tokenBytes, hookHash, adminHash, createdAt, updatedAt)
	if err != nil {
		return nil, err
	}
	return ws.WithVersion(version), nil
}

func (r *PostgresWorkspaceRepository) Update(ctx context.Context, ws *entity.Workspace) error {
	const query = `
		UPDATE workspaces
		SET slack_team_name = $2,
		    slack_bot_token_encrypted = $3,
		    webhook_secret_hash = $4,
		    admin_api_key_hash = $5,
		    updated_at = $6,
		    version = version + 1
		WHERE id = $1 AND version = $7
		RETURNING version;
	`
	exec := GetExecutor(ctx, r.pool)
	var version int64
	err := exec.QueryRow(ctx, query,
		ws.ID().UUID(),
		ws.SlackTeamName(),
		ws.SlackBotTokenEncrypted(),
		ws.WebhookSecretHash(),
		ws.AdminAPIKeyHash(),
		ws.UpdatedAt(),
		ws.Version(),
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return conflictOrMissing(ctx, exec, "workspaces", "id = $1", ws.ID().UUID())
	}
	if err != nil {
		return fmt.Errorf("updating workspace: %w", err)
	}
	ws.WithVersion(version)
	return nil
}
