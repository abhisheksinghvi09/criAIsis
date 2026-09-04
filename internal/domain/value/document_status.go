package value

import (
	"fmt"
	"strings"
)

// DocumentStatus tracks the asynchronous ingestion lifecycle of uploaded runbooks.
// Supporting explicit status values allows micro-batch chunking and worker retries without UI blocking.
type DocumentStatus string

const (
	// DocumentStatusPending: Uploaded and stored, awaiting chunking and vector embedding.
	DocumentStatusPending DocumentStatus = "pending"

	// DocumentStatusIndexed: Chunking, embedding, and tsvector generation completed successfully.
	DocumentStatusIndexed DocumentStatus = "indexed"

	// DocumentStatusFailed: Ingestion aborted due to LLM embedding rate limits, malformed markdown, or DB errors.
	DocumentStatusFailed DocumentStatus = "failed"
)

// ParseDocumentStatus validates document status strings from DB rows or worker events.
func ParseDocumentStatus(raw string) (DocumentStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "pending":
		return DocumentStatusPending, nil
	case "indexed":
		return DocumentStatusIndexed, nil
	case "failed":
		return DocumentStatusFailed, nil
	default:
		return "", fmt.Errorf("invalid document status '%s': expected pending, indexed, or failed", raw)
	}
}

// String returns the string representation.
func (s DocumentStatus) String() string {
	return string(s)
}

// IsIndexed returns true if the document has completed ingestion and is queryable via RAG.
func (s DocumentStatus) IsIndexed() bool {
	return s == DocumentStatusIndexed
}
