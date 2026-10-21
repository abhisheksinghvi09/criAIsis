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

type PostgresDocumentRepository struct {
	pool *pgxpool.Pool
}

var _ repository.DocumentRepository = (*PostgresDocumentRepository)(nil)

func NewDocumentRepository(pool *pgxpool.Pool) *PostgresDocumentRepository {
	return &PostgresDocumentRepository{pool: pool}
}

func (r *PostgresDocumentRepository) Create(ctx context.Context, doc *entity.Document) error {
	const query = `
		INSERT INTO documents (id, workspace_id, persona_id, title, raw_content, content_hash, status, chunk_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);
	`
	exec := GetExecutor(ctx, r.pool)
	_, err := exec.Exec(ctx, query,
		doc.ID(),
		doc.WorkspaceID().UUID(),
		doc.PersonaID(),
		doc.Title(),
		doc.RawContent(),
		doc.ContentHash(),
		doc.Status().String(),
		doc.ChunkCount(),
		doc.CreatedAt(),
		doc.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("inserting document: %w", err)
	}
	return nil
}

func (r *PostgresDocumentRepository) GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Document, error) {
	const query = `
		SELECT id, workspace_id, persona_id, title, raw_content, content_hash, status, chunk_count, created_at, updated_at
		FROM documents
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	return r.scanDocument(exec.QueryRow(ctx, query, wsID.UUID(), id))
}

func (r *PostgresDocumentRepository) GetByContentHash(ctx context.Context, wsID value.WorkspaceID, hash string) (*entity.Document, error) {
	const query = `
		SELECT id, workspace_id, persona_id, title, raw_content, content_hash, status, chunk_count, created_at, updated_at
		FROM documents
		WHERE workspace_id = $1 AND content_hash = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	return r.scanDocument(exec.QueryRow(ctx, query, wsID.UUID(), hash))
}

func (r *PostgresDocumentRepository) ListSummaries(ctx context.Context, wsID value.WorkspaceID) ([]repository.DocumentSummary, error) {
	rows, err := GetExecutor(ctx, r.pool).Query(ctx, `
		SELECT id, persona_id, title, status, chunk_count
		FROM documents
		WHERE workspace_id = $1
		ORDER BY created_at DESC`, wsID.UUID())
	if err != nil {
		return nil, fmt.Errorf("listing documents: %w", err)
	}
	defer rows.Close()

	var out []repository.DocumentSummary
	for rows.Next() {
		var d repository.DocumentSummary
		if err := rows.Scan(&d.ID, &d.PersonaID, &d.Title, &d.Status, &d.ChunkCount); err != nil {
			return nil, fmt.Errorf("scanning document summary: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *PostgresDocumentRepository) UpdateStatus(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID, status value.DocumentStatus, chunkCount int) error {
	const query = `
		UPDATE documents
		SET status = $3,
		    chunk_count = $4,
		    updated_at = $5
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	tag, err := exec.Exec(ctx, query,
		wsID.UUID(),
		id,
		status.String(),
		chunkCount,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("updating document status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("document '%s' not found for status update in workspace '%s'", id, wsID)
	}
	return nil
}

func (r *PostgresDocumentRepository) Delete(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) error {
	const query = `
		DELETE FROM documents
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	tag, err := exec.Exec(ctx, query, wsID.UUID(), id)
	if err != nil {
		return fmt.Errorf("deleting document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("document '%s' not found for deletion in workspace '%s'", id, wsID)
	}
	return nil
}

func (r *PostgresDocumentRepository) scanDocument(row pgx.Row) (*entity.Document, error) {
	var (
		id          uuid.UUID
		rawWsID     uuid.UUID
		personaID   uuid.UUID
		title       string
		rawContent  string
		contentHash string
		rawStatus   string
		chunkCount  int
		createdAt   time.Time
		updatedAt   time.Time
	)

	err := row.Scan(&id, &rawWsID, &personaID, &title, &rawContent, &contentHash, &rawStatus, &chunkCount, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("document not found")
		}
		return nil, fmt.Errorf("scanning document: %w", err)
	}

	parsedWsID, err := value.FromUUID(rawWsID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in document: %w", err)
	}

	status, err := value.ParseDocumentStatus(rawStatus)
	if err != nil {
		return nil, fmt.Errorf("invalid document status in db: %w", err)
	}

	return entity.ReconstituteDocument(
		id,
		parsedWsID,
		personaID,
		title,
		rawContent,
		contentHash,
		status,
		chunkCount,
		createdAt,
		updatedAt,
	), nil
}
