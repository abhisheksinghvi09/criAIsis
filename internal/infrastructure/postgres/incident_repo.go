package postgres

import (
	"context"
	"encoding/json"
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

type PostgresIncidentRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IncidentRepository = (*PostgresIncidentRepository)(nil)

func NewIncidentRepository(pool *pgxpool.Pool) *PostgresIncidentRepository {
	return &PostgresIncidentRepository{pool: pool}
}

func (r *PostgresIncidentRepository) Create(ctx context.Context, inc *entity.Incident) error {
	const query = `
		INSERT INTO incidents (
			id, workspace_id, title, description, slack_channel_id, slack_thread_ts,
			severity, status, created_by_slack_user_id, metadata, resolved_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);
	`
	exec := GetExecutor(ctx, r.pool)
	_, err := exec.Exec(ctx, query,
		inc.ID(),
		inc.WorkspaceID().UUID(),
		inc.Title(),
		inc.Description(),
		inc.SlackChannelID().String(),
		inc.SlackThreadTS().String(),
		inc.Severity().String(),
		inc.Status().String(),
		inc.CreatedBySlackUser().String(),
		inc.Metadata(),
		inc.ResolvedAt(),
		inc.CreatedAt(),
		inc.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("inserting incident: %w", err)
	}
	return nil
}

func (r *PostgresIncidentRepository) GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Incident, error) {
	const query = `
		SELECT id, workspace_id, title, description, slack_channel_id, slack_thread_ts,
		       severity, status, created_by_slack_user_id, metadata, resolved_at, created_at, updated_at
		FROM incidents
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	return r.scanIncident(exec.QueryRow(ctx, query, wsID.UUID(), id))
}

func (r *PostgresIncidentRepository) GetBySlackThread(ctx context.Context, wsID value.WorkspaceID, channelID value.SlackChannelID, threadTS value.SlackThreadTS) (*entity.Incident, error) {
	const query = `
		SELECT id, workspace_id, title, description, slack_channel_id, slack_thread_ts,
		       severity, status, created_by_slack_user_id, metadata, resolved_at, created_at, updated_at
		FROM incidents
		WHERE workspace_id = $1 AND slack_channel_id = $2 AND slack_thread_ts = $3;
	`
	exec := GetExecutor(ctx, r.pool)
	return r.scanIncident(exec.QueryRow(ctx, query, wsID.UUID(), channelID.String(), threadTS.String()))
}

func (r *PostgresIncidentRepository) ListActive(ctx context.Context, wsID value.WorkspaceID, limit, offset int) ([]*entity.Incident, error) {
	if limit <= 0 {
		limit = 20
	}
	const query = `
		SELECT id, workspace_id, title, description, slack_channel_id, slack_thread_ts,
		       severity, status, created_by_slack_user_id, metadata, resolved_at, created_at, updated_at
		FROM incidents
		WHERE workspace_id = $1 AND status = 'investigating'
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3;
	`
	exec := GetExecutor(ctx, r.pool)
	rows, err := exec.Query(ctx, query, wsID.UUID(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing active incidents: %w", err)
	}
	defer rows.Close()

	return r.scanIncidentList(rows)
}

func (r *PostgresIncidentRepository) ListRecent(ctx context.Context, wsID value.WorkspaceID, limit, offset int) ([]*entity.Incident, error) {
	if limit <= 0 {
		limit = 20
	}
	const query = `
		SELECT id, workspace_id, title, description, slack_channel_id, slack_thread_ts,
		       severity, status, created_by_slack_user_id, metadata, resolved_at, created_at, updated_at
		FROM incidents
		WHERE workspace_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3;
	`
	exec := GetExecutor(ctx, r.pool)
	rows, err := exec.Query(ctx, query, wsID.UUID(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing recent incidents: %w", err)
	}
	defer rows.Close()

	return r.scanIncidentList(rows)
}

func (r *PostgresIncidentRepository) Update(ctx context.Context, inc *entity.Incident) error {
	const query = `
		UPDATE incidents
		SET severity = $3,
		    status = $4,
		    metadata = $5,
		    resolved_at = $6,
		    updated_at = $7
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	tag, err := exec.Exec(ctx, query,
		inc.WorkspaceID().UUID(),
		inc.ID(),
		inc.Severity().String(),
		inc.Status().String(),
		inc.Metadata(),
		inc.ResolvedAt(),
		inc.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("updating incident: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("incident '%s' not found for update in workspace '%s'", inc.ID(), inc.WorkspaceID())
	}
	return nil
}

func (r *PostgresIncidentRepository) scanIncident(row pgx.Row) (*entity.Incident, error) {
	var (
		id          uuid.UUID
		rawWsID     uuid.UUID
		title       string
		description string
		channelID   string
		threadTS    string
		rawSeverity string
		rawStatus   string
		creatorID   string
		metadata    json.RawMessage
		resolvedAt  *time.Time
		createdAt   time.Time
		updatedAt   time.Time
	)

	err := row.Scan(
		&id, &rawWsID, &title, &description, &channelID, &threadTS,
		&rawSeverity, &rawStatus, &creatorID, &metadata, &resolvedAt, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("incident not found")
		}
		return nil, fmt.Errorf("scanning incident: %w", err)
	}

	wsID, err := value.FromUUID(rawWsID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in incident: %w", err)
	}

	slackChannelID, err := value.NewSlackChannelID(channelID)
	if err != nil {
		return nil, fmt.Errorf("invalid slack channel id: %w", err)
	}

	slackThreadTS, err := value.NewSlackThreadTS(threadTS)
	if err != nil {
		return nil, fmt.Errorf("invalid slack thread ts: %w", err)
	}

	sev, err := value.ParseSeverity(rawSeverity)
	if err != nil {
		return nil, fmt.Errorf("invalid severity in incident: %w", err)
	}

	status, err := value.ParseIncidentStatus(rawStatus)
	if err != nil {
		return nil, fmt.Errorf("invalid incident status: %w", err)
	}

	creatorSlackUser, err := value.NewSlackUserID(creatorID)
	if err != nil {
		return nil, fmt.Errorf("invalid creator slack user id: %w", err)
	}

	return entity.ReconstituteIncident(
		id,
		wsID,
		title,
		description,
		slackChannelID,
		slackThreadTS,
		sev,
		status,
		creatorSlackUser,
		metadata,
		resolvedAt,
		createdAt,
		updatedAt,
	), nil
}

func (r *PostgresIncidentRepository) scanIncidentList(rows pgx.Rows) ([]*entity.Incident, error) {
	var incidents []*entity.Incident
	for rows.Next() {
		var (
			id          uuid.UUID
			rawWsID     uuid.UUID
			title       string
			description string
			channelID   string
			threadTS    string
			rawSeverity string
			rawStatus   string
			creatorID   string
			metadata    json.RawMessage
			resolvedAt  *time.Time
			createdAt   time.Time
			updatedAt   time.Time
		)

		if err := rows.Scan(
			&id, &rawWsID, &title, &description, &channelID, &threadTS,
			&rawSeverity, &rawStatus, &creatorID, &metadata, &resolvedAt, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning incident row: %w", err)
		}

		wsID, err := value.FromUUID(rawWsID)
		if err != nil {
			return nil, fmt.Errorf("invalid workspace id: %w", err)
		}

		slackChannelID, err := value.NewSlackChannelID(channelID)
		if err != nil {
			return nil, fmt.Errorf("invalid channel id: %w", err)
		}

		slackThreadTS, err := value.NewSlackThreadTS(threadTS)
		if err != nil {
			return nil, fmt.Errorf("invalid thread ts: %w", err)
		}

		sev, err := value.ParseSeverity(rawSeverity)
		if err != nil {
			return nil, fmt.Errorf("invalid severity: %w", err)
		}

		status, err := value.ParseIncidentStatus(rawStatus)
		if err != nil {
			return nil, fmt.Errorf("invalid status: %w", err)
		}

		creatorSlackUser, err := value.NewSlackUserID(creatorID)
		if err != nil {
			return nil, fmt.Errorf("invalid creator user id: %w", err)
		}

		incidents = append(incidents, entity.ReconstituteIncident(
			id,
			wsID,
			title,
			description,
			slackChannelID,
			slackThreadTS,
			sev,
			status,
			creatorSlackUser,
			metadata,
			resolvedAt,
			createdAt,
			updatedAt,
		))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating incident rows: %w", err)
	}

	return incidents, nil
}
