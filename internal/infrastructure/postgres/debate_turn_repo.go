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

type PostgresDebateTurnRepository struct {
	pool *pgxpool.Pool
}

var _ repository.DebateTurnRepository = (*PostgresDebateTurnRepository)(nil)

func NewDebateTurnRepository(pool *pgxpool.Pool) *PostgresDebateTurnRepository {
	return &PostgresDebateTurnRepository{pool: pool}
}

func (r *PostgresDebateTurnRepository) Create(ctx context.Context, turn *entity.DebateTurn) error {
	const query = `
		INSERT INTO debate_turns (
			id, incident_id, workspace_id, persona_id, stage, turn_type,
			content, referenced_chunk_ids, slack_message_ts, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);
	`
	var pID *uuid.UUID
	if turn.PersonaID() != nil {
		id := *turn.PersonaID()
		pID = &id
	}

	exec := GetExecutor(ctx, r.pool)
	_, err := exec.Exec(ctx, query,
		turn.ID(),
		turn.IncidentID(),
		turn.WorkspaceID().UUID(),
		pID,
		turn.Stage().Int(),
		turn.TurnType().String(),
		turn.Content(),
		turn.ReferencedChunkIDs(),
		turn.SlackMessageTS(),
		turn.Metadata(),
		turn.CreatedAt(),
	)
	if err != nil {
		return fmt.Errorf("inserting debate turn: %w", err)
	}
	return nil
}

func (r *PostgresDebateTurnRepository) GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.DebateTurn, error) {
	const query = `
		SELECT id, incident_id, workspace_id, persona_id, stage, turn_type,
		       content, referenced_chunk_ids, slack_message_ts, metadata, created_at
		FROM debate_turns
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	var (
		turnID         uuid.UUID
		incID          uuid.UUID
		rawWsID        uuid.UUID
		personaID      *uuid.UUID
		rawStage       int
		rawTurnType    string
		content        string
		chunkIDs       []uuid.UUID
		slackMessageTS string
		metadata       json.RawMessage
		createdAt      time.Time
	)

	err := exec.QueryRow(ctx, query, wsID.UUID(), id).Scan(
		&turnID, &incID, &rawWsID, &personaID, &rawStage, &rawTurnType,
		&content, &chunkIDs, &slackMessageTS, &metadata, &createdAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("debate turn not found")
		}
		return nil, fmt.Errorf("scanning debate turn: %w", err)
	}

	parsedWsID, err := value.FromUUID(rawWsID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in debate turn: %w", err)
	}

	stage, err := value.ParseStage(rawStage)
	if err != nil {
		return nil, fmt.Errorf("invalid clash stage in db: %w", err)
	}

	turnType, err := value.ParseTurnType(rawTurnType)
	if err != nil {
		return nil, fmt.Errorf("invalid turn type in db: %w", err)
	}

	return entity.ReconstituteDebateTurn(
		turnID,
		incID,
		parsedWsID,
		personaID,
		stage,
		turnType,
		content,
		chunkIDs,
		slackMessageTS,
		metadata,
		createdAt,
	), nil
}

func (r *PostgresDebateTurnRepository) ListByIncident(ctx context.Context, wsID value.WorkspaceID, incidentID uuid.UUID) ([]*entity.DebateTurn, error) {
	const query = `
		SELECT id, incident_id, workspace_id, persona_id, stage, turn_type,
		       content, referenced_chunk_ids, slack_message_ts, metadata, created_at
		FROM debate_turns
		WHERE workspace_id = $1 AND incident_id = $2
		ORDER BY created_at ASC;
	`
	exec := GetExecutor(ctx, r.pool)
	rows, err := exec.Query(ctx, query, wsID.UUID(), incidentID)
	if err != nil {
		return nil, fmt.Errorf("listing debate turns by incident: %w", err)
	}
	defer rows.Close()

	var turns []*entity.DebateTurn
	for rows.Next() {
		var (
			turnID         uuid.UUID
			incID          uuid.UUID
			rawWsID        uuid.UUID
			personaID      *uuid.UUID
			rawStage       int
			rawTurnType    string
			content        string
			chunkIDs       []uuid.UUID
			slackMessageTS string
			metadata       json.RawMessage
			createdAt      time.Time
		)

		if err := rows.Scan(
			&turnID, &incID, &rawWsID, &personaID, &rawStage, &rawTurnType,
			&content, &chunkIDs, &slackMessageTS, &metadata, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning debate turn row: %w", err)
		}

		parsedWsID, err := value.FromUUID(rawWsID)
		if err != nil {
			return nil, fmt.Errorf("invalid workspace id in debate turn: %w", err)
		}

		stage, err := value.ParseStage(rawStage)
		if err != nil {
			return nil, fmt.Errorf("invalid stage in db: %w", err)
		}

		turnType, err := value.ParseTurnType(rawTurnType)
		if err != nil {
			return nil, fmt.Errorf("invalid turn type in db: %w", err)
		}

		turns = append(turns, entity.ReconstituteDebateTurn(
			turnID,
			incID,
			parsedWsID,
			personaID,
			stage,
			turnType,
			content,
			chunkIDs,
			slackMessageTS,
			metadata,
			createdAt,
		))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating debate turn rows: %w", err)
	}

	return turns, nil
}
