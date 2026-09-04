package value

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// CitationSnapshot preserves an immutable copy of runbook evidence at the exact moment a hypothesis is generated.
// Storing this snapshot in DebateTurn.metadata JSONB protects post-mortem audit trails even if the underlying document is later modified or purged.
type CitationSnapshot struct {
	ChunkID       uuid.UUID `json:"chunk_id"`
	DocumentTitle string    `json:"document_title"`
	Snippet       string    `json:"snippet"`
	Source        string    `json:"source,omitempty"`
}

// NewCitationSnapshot validates and constructs a citation record.
func NewCitationSnapshot(chunkID uuid.UUID, docTitle string, snippet string, source string) (CitationSnapshot, error) {
	if chunkID == uuid.Nil {
		return CitationSnapshot{}, errors.New("chunk id cannot be nil")
	}
	trimmedTitle := strings.TrimSpace(docTitle)
	if trimmedTitle == "" {
		return CitationSnapshot{}, errors.New("document title cannot be empty")
	}
	trimmedSnippet := strings.TrimSpace(snippet)
	if trimmedSnippet == "" {
		return CitationSnapshot{}, errors.New("citation snippet cannot be empty")
	}

	return CitationSnapshot{
		ChunkID:       chunkID,
		DocumentTitle: trimmedTitle,
		Snippet:       trimmedSnippet,
		Source:        strings.TrimSpace(source),
	}, nil
}
