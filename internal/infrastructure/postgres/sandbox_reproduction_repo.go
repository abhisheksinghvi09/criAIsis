package postgres

import (
	"context"
	"database/sql"
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

type PostgresSandboxReproductionRepository struct {
	pool *pgxpool.Pool
}

var _ repository.SandboxReproductionRepository = (*PostgresSandboxReproductionRepository)(nil)

func NewSandboxReproductionRepository(pool *pgxpool.Pool) *PostgresSandboxReproductionRepository {
	return &PostgresSandboxReproductionRepository{pool: pool}
}

func (r *PostgresSandboxReproductionRepository) Create(ctx context.Context, sr *entity.SandboxReproduction) error {
	const query = `
		INSERT INTO sandbox_reproductions (id, incident_id, workspace_id, status, scenario_id, container_ref, created_at, ready_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);
	`
	exec := GetExecutor(ctx, r.pool)
	var scenarioID *string
	if sr.ScenarioID() != "" {
		s := sr.ScenarioID()
		scenarioID = &s
	}
	var containerRef *string
	if sr.ContainerRef() != "" {
		c := sr.ContainerRef()
		containerRef = &c
	}

	_, err := exec.Exec(ctx, query,
		sr.ID(),
		sr.IncidentID(),
		sr.WorkspaceID().UUID(),
		sr.Status().String(),
		scenarioID,
		containerRef,
		sr.CreatedAt(),
		sr.ReadyAt(),
		sr.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("inserting sandbox reproduction: %w", err)
	}
	return nil
}

func (r *PostgresSandboxReproductionRepository) GetByID(ctx context.Context, id uuid.UUID) (*entity.SandboxReproduction, error) {
	const query = `
		SELECT id, incident_id, workspace_id, status, scenario_id, container_ref, created_at, ready_at, updated_at
		FROM sandbox_reproductions
		WHERE id = $1;
	`
	exec := GetExecutor(ctx, r.pool)
	var (
		rawID         uuid.UUID
		incidentID    uuid.UUID
		workspaceUUID uuid.UUID
		statusRaw     string
		scenarioID    sql.NullString
		containerRef  sql.NullString
		createdAt     time.Time
		readyAt       *time.Time
		updatedAt     time.Time
	)

	err := exec.QueryRow(ctx, query, id).Scan(
		&rawID,
		&incidentID,
		&workspaceUUID,
		&statusRaw,
		&scenarioID,
		&containerRef,
		&createdAt,
		&readyAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("sandbox reproduction '%s' not found", id)
		}
		return nil, fmt.Errorf("querying sandbox reproduction by id: %w", err)
	}

	wsID, err := value.FromUUID(workspaceUUID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in db: %w", err)
	}

	status, err := value.ParseSandboxStatus(statusRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid sandbox status in db: %w", err)
	}

	return entity.ReconstituteSandboxReproduction(
		rawID,
		incidentID,
		wsID,
		status,
		scenarioID.String,
		containerRef.String,
		createdAt,
		readyAt,
		updatedAt,
	), nil
}

func (r *PostgresSandboxReproductionRepository) GetActiveForIncident(ctx context.Context, incidentID uuid.UUID) (*entity.SandboxReproduction, error) {
	const query = `
		SELECT id, incident_id, workspace_id, status, scenario_id, container_ref, created_at, ready_at, updated_at
		FROM sandbox_reproductions
		WHERE incident_id = $1 AND status IN ('Provisioning', 'Ready')
		ORDER BY created_at DESC
		LIMIT 1;
	`
	exec := GetExecutor(ctx, r.pool)
	var (
		rawID         uuid.UUID
		incID         uuid.UUID
		workspaceUUID uuid.UUID
		statusRaw     string
		scenarioID    sql.NullString
		containerRef  sql.NullString
		createdAt     time.Time
		readyAt       *time.Time
		updatedAt     time.Time
	)

	err := exec.QueryRow(ctx, query, incidentID).Scan(
		&rawID,
		&incID,
		&workspaceUUID,
		&statusRaw,
		&scenarioID,
		&containerRef,
		&createdAt,
		&readyAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No active reproduction
		}
		return nil, fmt.Errorf("querying active sandbox reproduction: %w", err)
	}

	wsID, err := value.FromUUID(workspaceUUID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in db: %w", err)
	}

	status, err := value.ParseSandboxStatus(statusRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid sandbox status in db: %w", err)
	}

	return entity.ReconstituteSandboxReproduction(
		rawID,
		incID,
		wsID,
		status,
		scenarioID.String,
		containerRef.String,
		createdAt,
		readyAt,
		updatedAt,
	), nil
}

func (r *PostgresSandboxReproductionRepository) Update(ctx context.Context, sr *entity.SandboxReproduction) error {
	const query = `
		UPDATE sandbox_reproductions
		SET status = $2,
		    scenario_id = $3,
		    container_ref = $4,
		    ready_at = $5,
		    updated_at = $6
		WHERE id = $1;
	`
	exec := GetExecutor(ctx, r.pool)
	var scenarioID *string
	if sr.ScenarioID() != "" {
		s := sr.ScenarioID()
		scenarioID = &s
	}
	var containerRef *string
	if sr.ContainerRef() != "" {
		c := sr.ContainerRef()
		containerRef = &c
	}

	tag, err := exec.Exec(ctx, query,
		sr.ID(),
		sr.Status().String(),
		scenarioID,
		containerRef,
		sr.ReadyAt(),
		sr.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("updating sandbox reproduction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("sandbox reproduction '%s' not found for update", sr.ID())
	}
	return nil
}
