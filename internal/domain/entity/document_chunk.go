package entity

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// DocumentChunk is a segmented excerpt of a runbook with an associated dense embedding and search vector.
type DocumentChunk struct {
	id          uuid.UUID
	workspaceID value.WorkspaceID
	personaID   uuid.UUID
	documentID  uuid.UUID
	chunkIndex  int
	chunkText   string
	tokenCount  int
	embedding   value.EmbeddingVector
	metadata    json.RawMessage
	createdAt   time.Time
}

func NewDocumentChunk(
	workspaceID value.WorkspaceID,
	personaID uuid.UUID,
	documentID uuid.UUID,
	chunkIndex int,
	chunkText string,
	tokenCount int,
	embedding value.EmbeddingVector,
	metadata json.RawMessage,
) (*DocumentChunk, error) {
	if workspaceID.IsZero() {
		return nil, errors.New("workspace id cannot be zero")
	}
	if personaID == uuid.Nil {
		return nil, errors.New("persona id cannot be nil")
	}
	if documentID == uuid.Nil {
		return nil, errors.New("document id cannot be nil")
	}
	if chunkIndex < 0 {
		return nil, errors.New("chunk index cannot be negative")
	}
	if strings.TrimSpace(chunkText) == "" {
		return nil, errors.New("chunk text cannot be empty")
	}
	if tokenCount <= 0 {
		return nil, errors.New("token count must be positive")
	}
	if len(embedding) != value.ExpectedEmbeddingDimensions {
		return nil, errors.New("invalid embedding dimensions")
	}

	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	return &DocumentChunk{
		id:          uuid.New(),
		workspaceID: workspaceID,
		personaID:   personaID,
		documentID:  documentID,
		chunkIndex:  chunkIndex,
		chunkText:   chunkText,
		tokenCount:  tokenCount,
		embedding:   embedding,
		metadata:    metadata,
		createdAt:   time.Now().UTC(),
	}, nil
}

func ReconstituteDocumentChunk(
	id uuid.UUID,
	workspaceID value.WorkspaceID,
	personaID uuid.UUID,
	documentID uuid.UUID,
	chunkIndex int,
	chunkText string,
	tokenCount int,
	embedding value.EmbeddingVector,
	metadata json.RawMessage,
	createdAt time.Time,
) *DocumentChunk {
	return &DocumentChunk{
		id:          id,
		workspaceID: workspaceID,
		personaID:   personaID,
		documentID:  documentID,
		chunkIndex:  chunkIndex,
		chunkText:   chunkText,
		tokenCount:  tokenCount,
		embedding:   embedding,
		metadata:    metadata,
		createdAt:   createdAt,
	}
}

func (c *DocumentChunk) ID() uuid.UUID                  { return c.id }
func (c *DocumentChunk) WorkspaceID() value.WorkspaceID { return c.workspaceID }
func (c *DocumentChunk) PersonaID() uuid.UUID           { return c.personaID }
func (c *DocumentChunk) DocumentID() uuid.UUID          { return c.documentID }
func (c *DocumentChunk) ChunkIndex() int                { return c.chunkIndex }
func (c *DocumentChunk) ChunkText() string              { return c.chunkText }
func (c *DocumentChunk) TokenCount() int                { return c.tokenCount }
func (c *DocumentChunk) Embedding() value.EmbeddingVector { return c.embedding }
func (c *DocumentChunk) Metadata() json.RawMessage     { return c.metadata }
func (c *DocumentChunk) CreatedAt() time.Time           { return c.createdAt }
