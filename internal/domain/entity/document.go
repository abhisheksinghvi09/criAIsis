package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// Document represents an uploaded runbook, architectural guide, or troubleshooting document.
// It tracks its asynchronous chunking lifecycle from initial upload through vector indexing.
type Document struct {
	id          uuid.UUID
	workspaceID value.WorkspaceID
	personaID   uuid.UUID
	title       string
	rawContent  string
	contentHash string
	status      value.DocumentStatus
	chunkCount  int
	createdAt   time.Time
	updatedAt   time.Time
}

// NewDocument constructs an uploaded runbook in the pending state before chunking starts.
func NewDocument(
	workspaceID value.WorkspaceID,
	personaID uuid.UUID,
	title string,
	rawContent string,
) (*Document, error) {
	if workspaceID.IsZero() {
		return nil, errors.New("workspace id cannot be zero")
	}
	if personaID == uuid.Nil {
		return nil, errors.New("persona id cannot be nil")
	}
	trimmedTitle := strings.TrimSpace(title)
	if trimmedTitle == "" {
		return nil, errors.New("title cannot be empty")
	}
	if strings.TrimSpace(rawContent) == "" {
		return nil, errors.New("raw content cannot be empty")
	}

	hash := CalculateContentHash(rawContent)
	now := time.Now().UTC()

	return &Document{
		id:          uuid.New(),
		workspaceID: workspaceID,
		personaID:   personaID,
		title:       trimmedTitle,
		rawContent:  rawContent,
		contentHash: hash,
		status:      value.DocumentStatusPending,
		chunkCount:  0,
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

// ReconstituteDocument re-assembles an existing document from SQL persistence.
func ReconstituteDocument(
	id uuid.UUID,
	workspaceID value.WorkspaceID,
	personaID uuid.UUID,
	title string,
	rawContent string,
	contentHash string,
	status value.DocumentStatus,
	chunkCount int,
	createdAt time.Time,
	updatedAt time.Time,
) *Document {
	return &Document{
		id:          id,
		workspaceID: workspaceID,
		personaID:   personaID,
		title:       title,
		rawContent:  rawContent,
		contentHash: contentHash,
		status:      status,
		chunkCount:  chunkCount,
		createdAt:   createdAt,
		updatedAt:   updatedAt,
	}
}

func (d *Document) ID() uuid.UUID                  { return d.id }
func (d *Document) WorkspaceID() value.WorkspaceID { return d.workspaceID }
func (d *Document) PersonaID() uuid.UUID           { return d.personaID }
func (d *Document) Title() string                  { return d.title }
func (d *Document) RawContent() string             { return d.rawContent }
func (d *Document) ContentHash() string            { return d.contentHash }
func (d *Document) Status() value.DocumentStatus   { return d.status }
func (d *Document) ChunkCount() int                { return d.chunkCount }
func (d *Document) CreatedAt() time.Time           { return d.createdAt }
func (d *Document) UpdatedAt() time.Time           { return d.updatedAt }

// MarkIndexed updates the document status to indexed and stores the total number of generated chunks.
func (d *Document) MarkIndexed(chunkCount int) {
	d.status = value.DocumentStatusIndexed
	d.chunkCount = chunkCount
	d.updatedAt = time.Now().UTC()
}

// MarkFailed transitions the document status to failed when embedding or chunking errors occur.
func (d *Document) MarkFailed() {
	d.status = value.DocumentStatusFailed
	d.updatedAt = time.Now().UTC()
}

// CalculateContentHash creates a deterministic SHA-256 string for duplicate detection.
func CalculateContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
