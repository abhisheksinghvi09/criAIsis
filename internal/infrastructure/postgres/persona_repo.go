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

type PostgresPersonaRepository struct {
	pool *pgxpool.Pool
}

var _ repository.PersonaRepository = (*PostgresPersonaRepository)(nil)

func NewPersonaRepository(pool *pgxpool.Pool) *PostgresPersonaRepository {
	return &PostgresPersonaRepository{pool: pool}
}

func (r *PostgresPersonaRepository) Create(ctx context.Context, persona *entity.Persona) error {
	const query = `
		INSERT INTO personas (id, workspace_id, key, display_name, system_prompt, is_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
	`
	exec := GetExecutor(ctx, r.pool)
	_, err := exec.Exec(ctx, query,
		persona.ID(),
		persona.WorkspaceID().UUID(),
		persona.Key().String(),
		persona.DisplayName(),
		persona.SystemPrompt(),
		persona.IsEnabled(),
		persona.CreatedAt(),
		persona.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("inserting persona: %w", err)
	}
	return nil
}

func (r *PostgresPersonaRepository) GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Persona, error) {
	const query = `
		SELECT id, workspace_id, key, display_name, system_prompt, is_enabled, created_at, updated_at
		FROM personas
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	return r.scanPersona(exec.QueryRow(ctx, query, wsID.UUID(), id))
}

func (r *PostgresPersonaRepository) GetByKey(ctx context.Context, wsID value.WorkspaceID, key value.PersonaKey) (*entity.Persona, error) {
	const query = `
		SELECT id, workspace_id, key, display_name, system_prompt, is_enabled, created_at, updated_at
		FROM personas
		WHERE workspace_id = $1 AND key = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	return r.scanPersona(exec.QueryRow(ctx, query, wsID.UUID(), key.String()))
}

func (r *PostgresPersonaRepository) ListByWorkspace(ctx context.Context, wsID value.WorkspaceID) ([]*entity.Persona, error) {
	const query = `
		SELECT id, workspace_id, key, display_name, system_prompt, is_enabled, created_at, updated_at
		FROM personas
		WHERE workspace_id = $1
		ORDER BY created_at ASC;
	`
	exec := GetExecutor(ctx, r.pool)
	rows, err := exec.Query(ctx, query, wsID.UUID())
	if err != nil {
		return nil, fmt.Errorf("querying personas by workspace: %w", err)
	}
	defer rows.Close()

	var personas []*entity.Persona
	for rows.Next() {
		var (
			id           uuid.UUID
			rawWsID      uuid.UUID
			rawKey       string
			displayName  string
			systemPrompt string
			isEnabled    bool
			createdAt    time.Time
			updatedAt    time.Time
		)

		if err := rows.Scan(&id, &rawWsID, &rawKey, &displayName, &systemPrompt, &isEnabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scanning persona row: %w", err)
		}

		parsedWsID, err := value.FromUUID(rawWsID)
		if err != nil {
			return nil, fmt.Errorf("invalid workspace id in persona row: %w", err)
		}

		key, err := value.ParsePersonaKey(rawKey)
		if err != nil {
			return nil, fmt.Errorf("invalid persona key in db: %w", err)
		}

		personas = append(personas, entity.ReconstitutePersona(
			id,
			parsedWsID,
			key,
			displayName,
			systemPrompt,
			isEnabled,
			createdAt,
			updatedAt,
		))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating persona rows: %w", err)
	}

	return personas, nil
}

func (r *PostgresPersonaRepository) Update(ctx context.Context, persona *entity.Persona) error {
	const query = `
		UPDATE personas
		SET display_name = $3,
		    system_prompt = $4,
		    is_enabled = $5,
		    updated_at = $6
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	tag, err := exec.Exec(ctx, query,
		persona.WorkspaceID().UUID(),
		persona.ID(),
		persona.DisplayName(),
		persona.SystemPrompt(),
		persona.IsEnabled(),
		persona.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("updating persona: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("persona '%s' not found for update in workspace '%s'", persona.ID(), persona.WorkspaceID())
	}
	return nil
}

func (r *PostgresPersonaRepository) scanPersona(row pgx.Row) (*entity.Persona, error) {
	var (
		id           uuid.UUID
		rawWsID      uuid.UUID
		rawKey       string
		displayName  string
		systemPrompt string
		isEnabled    bool
		createdAt    time.Time
		updatedAt    time.Time
	)

	err := row.Scan(&id, &rawWsID, &rawKey, &displayName, &systemPrompt, &isEnabled, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("persona not found")
		}
		return nil, fmt.Errorf("scanning persona: %w", err)
	}

	parsedWsID, err := value.FromUUID(rawWsID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in persona row: %w", err)
	}

	key, err := value.ParsePersonaKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("invalid persona key in db: %w", err)
	}

	return entity.ReconstitutePersona(
		id,
		parsedWsID,
		key,
		displayName,
		systemPrompt,
		isEnabled,
		createdAt,
		updatedAt,
	), nil
}
