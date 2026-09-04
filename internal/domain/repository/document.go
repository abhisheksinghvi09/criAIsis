package repository

import (
	"context"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// DocumentRepository defines the persistence contract for uploaded runbook knowledge bases.
type DocumentRepository interface {
	// Create persists an uploaded runbook document record in 'pending' status.
	Create(ctx context.Context, doc *entity.Document) error

	// GetByID retrieves a document by ID within a tenant.
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Document, error)

	// GetByContentHash checks whether an identical document content hash already exists for deduplication.
	GetByContentHash(ctx context.Context, wsID value.WorkspaceID, hash string) (*entity.Document, error)

	// ListByPersona retrieves all runbooks assigned to a specific specialist persona.
	ListByPersona(ctx context.Context, wsID value.WorkspaceID, personaID uuid.UUID) ([]*entity.Document, error)

	// UpdateStatus updates the asynchronous ingestion lifecycle state and chunk count.
	UpdateStatus(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID, status value.DocumentStatus, chunkCount int) error

	// Delete removes a runbook and cascades deletion to all associated chunks.
	Delete(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) error
}

// SearchResult bundles a matched document chunk with its hybrid rank score and parent document title.
type SearchResult struct {
	Chunk         *entity.DocumentChunk
	Score         float64
	DocumentTitle string
}

// ChunkRepository defines the persistence and similarity search contract for segmented vector chunks.
type ChunkRepository interface {
	// BatchCreate streams high-throughput chunk inserts using PostgreSQL binary copy protocol.
	BatchCreate(ctx context.Context, chunks []*entity.DocumentChunk) error

	// GetByID retrieves a specific chunk by primary key.
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.DocumentChunk, error)

	// ListByDocument retrieves all ordered chunks belonging to a single runbook.
	ListByDocument(ctx context.Context, wsID value.WorkspaceID, docID uuid.UUID) ([]*entity.DocumentChunk, error)

	// DeleteByDocument removes all chunks associated with a specific runbook.
	DeleteByDocument(ctx context.Context, wsID value.WorkspaceID, docID uuid.UUID) error

	// SearchHybrid executes Reciprocal Rank Fusion (RRF) combining dense HNSW embeddings and sparse tsvector full-text search.
	// Scoped strictly to (workspace_id, persona_id) to prevent cross-persona knowledge contamination.
	SearchHybrid(
		ctx context.Context,
		wsID value.WorkspaceID,
		personaID uuid.UUID,
		queryText string,
		queryEmbedding value.EmbeddingVector,
		limit int,
	) ([]*SearchResult, error)
}
