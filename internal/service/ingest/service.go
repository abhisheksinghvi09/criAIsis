package ingest

import (
	"context"
	"fmt"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/llm"
)

// Embedder is the slice of the embedding contract ingestion depends on.
type Embedder = llm.EmbeddingProvider

// Service drives the asynchronous document ingestion lifecycle:
// pending -> chunk -> embed -> persist -> indexed (or failed).
type Service struct {
	docs     repository.DocumentRepository
	chunks   repository.ChunkRepository
	embedder llm.EmbeddingProvider
}

// NewService wires the ingestion pipeline.
func NewService(docs repository.DocumentRepository, chunks repository.ChunkRepository, embedder llm.EmbeddingProvider) *Service {
	return &Service{docs: docs, chunks: chunks, embedder: embedder}
}

// IndexNew persists a new document in 'pending' status and then indexes it.
func (s *Service) IndexNew(ctx context.Context, doc *entity.Document) error {
	if err := s.docs.Create(ctx, doc); err != nil {
		return fmt.Errorf("creating document %q: %w", doc.Title(), err)
	}
	return s.Index(ctx, doc)
}

// Index chunks and embeds a pending document, then records the outcome.
// A failure is persisted as 'failed' so the dashboard never shows a document
// stuck in 'pending' with no explanation.
func (s *Service) Index(ctx context.Context, doc *entity.Document) error {
	count, err := s.index(ctx, doc)
	if err != nil {
		doc.MarkFailed()
		if updateErr := s.docs.UpdateStatus(ctx, doc.WorkspaceID(), doc.ID(), value.DocumentStatusFailed, 0); updateErr != nil {
			return fmt.Errorf("indexing failed (%w) and status update failed: %w", err, updateErr)
		}
		return err
	}

	doc.MarkIndexed(count)
	return s.docs.UpdateStatus(ctx, doc.WorkspaceID(), doc.ID(), value.DocumentStatusIndexed, count)
}

// index performs the work that may fail, keeping Index's status handling readable.
func (s *Service) index(ctx context.Context, doc *entity.Document) (int, error) {
	segments := Split(doc.RawContent())
	if len(segments) == 0 {
		return 0, fmt.Errorf("document %q produced no chunks", doc.Title())
	}

	texts := make([]string, len(segments))
	for i, seg := range segments {
		texts[i] = seg.Text
	}

	embeddings, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("embedding %q: %w", doc.Title(), err)
	}
	if len(embeddings) != len(segments) {
		return 0, fmt.Errorf("embedding %q: got %d vectors for %d chunks", doc.Title(), len(embeddings), len(segments))
	}

	chunks, err := buildChunks(doc, segments, embeddings)
	if err != nil {
		return 0, err
	}
	if err := s.chunks.BatchCreate(ctx, chunks); err != nil {
		return 0, fmt.Errorf("persisting chunks for %q: %w", doc.Title(), err)
	}
	return len(chunks), nil
}

// buildChunks assembles domain chunk entities from segments and their embeddings.
func buildChunks(doc *entity.Document, segments []Chunk, embeddings []value.EmbeddingVector) ([]*entity.DocumentChunk, error) {
	chunks := make([]*entity.DocumentChunk, 0, len(segments))
	for i, seg := range segments {
		chunk, err := entity.NewDocumentChunk(
			doc.WorkspaceID(),
			doc.PersonaID(),
			doc.ID(),
			i,
			seg.Text,
			seg.TokenCount,
			embeddings[i],
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("building chunk %d of %q: %w", i, doc.Title(), err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}
