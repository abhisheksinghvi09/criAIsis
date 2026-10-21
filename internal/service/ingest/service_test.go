package ingest_test

import (
	"context"
	"strings"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/llm"
	"criaisis/internal/service/ingest"

	"github.com/google/uuid"
)

type docsStub struct{ repository.DocumentRepository }

func (docsStub) Create(context.Context, *entity.Document) error { return nil }
func (docsStub) UpdateStatus(context.Context, value.WorkspaceID, uuid.UUID, value.DocumentStatus, int) error {
	return nil
}

type chunksStub struct {
	repository.ChunkRepository
	stored int
}

func (c *chunksStub) BatchCreate(_ context.Context, chunks []*entity.DocumentChunk) error {
	c.stored = len(chunks)
	return nil
}

type recordingEmbedder struct{ batches []int }

func (r *recordingEmbedder) Embed(ctx context.Context, texts []string) ([]value.EmbeddingVector, error) {
	r.batches = append(r.batches, len(texts))
	return llm.FakeEmbedder{}.Embed(ctx, texts)
}

func TestIndexNew_EmbedsLargeRunbooksInBoundedBatches(t *testing.T) {
	// ~2KB paragraphs, so every paragraph lands in its own chunk or two.
	content := strings.Repeat(strings.Repeat("word ", 400)+"\n\n", 150)
	doc, err := entity.NewDocument(value.NewWorkspaceID(), uuid.New(), "Big runbook", content)
	if err != nil {
		t.Fatal(err)
	}
	embedder, chunks := &recordingEmbedder{}, &chunksStub{}

	if err := ingest.NewService(docsStub{}, chunks, embedder).IndexNew(context.Background(), doc); err != nil {
		t.Fatalf("IndexNew: %v", err)
	}

	if len(embedder.batches) < 2 {
		t.Fatalf("expected several embedding requests, got %v", embedder.batches)
	}
	total := 0
	for _, n := range embedder.batches {
		if n > 64 {
			t.Errorf("batch of %d exceeds the 64 chunk limit", n)
		}
		total += n
	}
	if chunks.stored != total {
		t.Errorf("stored %d chunks but embedded %d", chunks.stored, total)
	}
}
