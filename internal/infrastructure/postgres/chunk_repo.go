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
	pgvector "github.com/pgvector/pgvector-go"
)

type PostgresChunkRepository struct {
	pool *pgxpool.Pool
}

var _ repository.ChunkRepository = (*PostgresChunkRepository)(nil)

func NewChunkRepository(pool *pgxpool.Pool) *PostgresChunkRepository {
	return &PostgresChunkRepository{pool: pool}
}

// BatchCreate utilizes the high-throughput PostgreSQL binary COPY protocol to stream chunk inserts.
func (r *PostgresChunkRepository) BatchCreate(ctx context.Context, chunks []*entity.DocumentChunk) error {
	if len(chunks) == 0 {
		return nil
	}

	columns := []string{
		"id",
		"workspace_id",
		"persona_id",
		"document_id",
		"chunk_index",
		"chunk_text",
		"token_count",
		"embedding",
		"metadata",
		"created_at",
	}

	rows := make([][]any, len(chunks))
	for i, c := range chunks {
		vec := pgvector.NewVector(c.Embedding().Slice())
		rows[i] = []any{
			c.ID(),
			c.WorkspaceID().UUID(),
			c.PersonaID(),
			c.DocumentID(),
			c.ChunkIndex(),
			c.ChunkText(),
			c.TokenCount(),
			vec,
			c.Metadata(),
			c.CreatedAt(),
		}
	}

	exec := GetExecutor(ctx, r.pool)
	_, err := exec.CopyFrom(
		ctx,
		pgx.Identifier{"document_chunks"},
		columns,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copying document chunks: %w", err)
	}

	return nil
}

func (r *PostgresChunkRepository) GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.DocumentChunk, error) {
	const query = `
		SELECT id, workspace_id, persona_id, document_id, chunk_index, chunk_text, token_count, embedding, metadata, created_at
		FROM document_chunks
		WHERE workspace_id = $1 AND id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	return r.scanChunk(exec.QueryRow(ctx, query, wsID.UUID(), id))
}

func (r *PostgresChunkRepository) ListByDocument(ctx context.Context, wsID value.WorkspaceID, docID uuid.UUID) ([]*entity.DocumentChunk, error) {
	const query = `
		SELECT id, workspace_id, persona_id, document_id, chunk_index, chunk_text, token_count, embedding, metadata, created_at
		FROM document_chunks
		WHERE workspace_id = $1 AND document_id = $2
		ORDER BY chunk_index ASC;
	`
	exec := GetExecutor(ctx, r.pool)
	rows, err := exec.Query(ctx, query, wsID.UUID(), docID)
	if err != nil {
		return nil, fmt.Errorf("listing chunks by document: %w", err)
	}
	defer rows.Close()

	var chunks []*entity.DocumentChunk
	for rows.Next() {
		var (
			id         uuid.UUID
			rawWsID    uuid.UUID
			personaID  uuid.UUID
			documentID uuid.UUID
			chunkIndex int
			chunkText  string
			tokenCount int
			vec        pgvector.Vector
			metadata   json.RawMessage
			createdAt  time.Time
		)

		if err := rows.Scan(&id, &rawWsID, &personaID, &documentID, &chunkIndex, &chunkText, &tokenCount, &vec, &metadata, &createdAt); err != nil {
			return nil, fmt.Errorf("scanning chunk row: %w", err)
		}

		parsedWsID, err := value.FromUUID(rawWsID)
		if err != nil {
			return nil, fmt.Errorf("invalid workspace id in chunk: %w", err)
		}

		embedding, err := value.NewEmbeddingVector(vec.Slice())
		if err != nil {
			return nil, fmt.Errorf("invalid embedding in chunk: %w", err)
		}

		chunks = append(chunks, entity.ReconstituteDocumentChunk(
			id,
			parsedWsID,
			personaID,
			documentID,
			chunkIndex,
			chunkText,
			tokenCount,
			embedding,
			metadata,
			createdAt,
		))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chunk rows: %w", err)
	}

	return chunks, nil
}

func (r *PostgresChunkRepository) DeleteByDocument(ctx context.Context, wsID value.WorkspaceID, docID uuid.UUID) error {
	const query = `
		DELETE FROM document_chunks
		WHERE workspace_id = $1 AND document_id = $2;
	`
	exec := GetExecutor(ctx, r.pool)
	_, err := exec.Exec(ctx, query, wsID.UUID(), docID)
	if err != nil {
		return fmt.Errorf("deleting chunks by document: %w", err)
	}
	return nil
}

// SearchHybrid executes Reciprocal Rank Fusion (RRF) combining dense HNSW vector search
// and sparse tsvector keyword search inside PostgreSQL.
func (r *PostgresChunkRepository) SearchHybrid(
	ctx context.Context,
	wsID value.WorkspaceID,
	personaID uuid.UUID,
	queryText string,
	queryEmbedding value.EmbeddingVector,
	limit int,
) ([]*repository.SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}

	const query = `
		WITH dense_search AS (
			SELECT id, document_id, chunk_index, chunk_text, token_count, embedding, metadata, created_at,
			       ROW_NUMBER() OVER (ORDER BY embedding <=> $4) as rank
			FROM document_chunks
			WHERE workspace_id = $1 AND persona_id = $2
			ORDER BY embedding <=> $4
			LIMIT 20
		),
		sparse_search AS (
			SELECT id, document_id, chunk_index, chunk_text, token_count, embedding, metadata, created_at,
			       ROW_NUMBER() OVER (ORDER BY ts_rank_cd(tsv, plainto_tsquery('english', $3)) DESC) as rank
			FROM document_chunks
			WHERE workspace_id = $1 AND persona_id = $2 AND tsv @@ plainto_tsquery('english', $3)
			ORDER BY ts_rank_cd(tsv, plainto_tsquery('english', $3)) DESC
			LIMIT 20
		)
		SELECT 
			COALESCE(d.id, s.id) as id,
			COALESCE(d.document_id, s.document_id) as document_id,
			COALESCE(d.chunk_index, s.chunk_index) as chunk_index,
			COALESCE(d.chunk_text, s.chunk_text) as chunk_text,
			COALESCE(d.token_count, s.token_count) as token_count,
			COALESCE(d.embedding, s.embedding) as embedding,
			COALESCE(d.metadata, s.metadata) as metadata,
			COALESCE(d.created_at, s.created_at) as created_at,
			doc.title as document_title,
			(COALESCE(1.0 / (60.0 + d.rank), 0.0) + COALESCE(1.0 / (60.0 + s.rank), 0.0)) as rrf_score
		FROM dense_search d
		FULL OUTER JOIN sparse_search s ON d.id = s.id
		JOIN documents doc ON doc.id = COALESCE(d.document_id, s.document_id)
		ORDER BY rrf_score DESC
		LIMIT $5;
	`

	vec := pgvector.NewVector(queryEmbedding.Slice())
	exec := GetExecutor(ctx, r.pool)
	rows, err := exec.Query(ctx, query, wsID.UUID(), personaID, queryText, vec, limit)
	if err != nil {
		return nil, fmt.Errorf("executing hybrid search: %w", err)
	}
	defer rows.Close()

	var results []*repository.SearchResult
	for rows.Next() {
		var (
			id         uuid.UUID
			documentID uuid.UUID
			chunkIndex int
			chunkText  string
			tokenCount int
			chunkVec   pgvector.Vector
			metadata   json.RawMessage
			createdAt  time.Time
			docTitle   string
			score      float64
		)

		if err := rows.Scan(&id, &documentID, &chunkIndex, &chunkText, &tokenCount, &chunkVec, &metadata, &createdAt, &docTitle, &score); err != nil {
			return nil, fmt.Errorf("scanning hybrid search row: %w", err)
		}

		embedding, err := value.NewEmbeddingVector(chunkVec.Slice())
		if err != nil {
			return nil, fmt.Errorf("invalid embedding from db: %w", err)
		}

		chunk := entity.ReconstituteDocumentChunk(
			id,
			wsID,
			personaID,
			documentID,
			chunkIndex,
			chunkText,
			tokenCount,
			embedding,
			metadata,
			createdAt,
		)

		results = append(results, &repository.SearchResult{
			Chunk:         chunk,
			Score:         score,
			DocumentTitle: docTitle,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating hybrid search rows: %w", err)
	}

	return results, nil
}

func (r *PostgresChunkRepository) scanChunk(row pgx.Row) (*entity.DocumentChunk, error) {
	var (
		id         uuid.UUID
		rawWsID    uuid.UUID
		personaID  uuid.UUID
		documentID uuid.UUID
		chunkIndex int
		chunkText  string
		tokenCount int
		vec        pgvector.Vector
		metadata   json.RawMessage
		createdAt  time.Time
	)

	err := row.Scan(&id, &rawWsID, &personaID, &documentID, &chunkIndex, &chunkText, &tokenCount, &vec, &metadata, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("document chunk not found")
		}
		return nil, fmt.Errorf("scanning chunk: %w", err)
	}

	parsedWsID, err := value.FromUUID(rawWsID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in chunk: %w", err)
	}

	embedding, err := value.NewEmbeddingVector(vec.Slice())
	if err != nil {
		return nil, fmt.Errorf("invalid embedding dimensions in chunk: %w", err)
	}

	return entity.ReconstituteDocumentChunk(
		id,
		parsedWsID,
		personaID,
		documentID,
		chunkIndex,
		chunkText,
		tokenCount,
		embedding,
		metadata,
		createdAt,
	), nil
}
