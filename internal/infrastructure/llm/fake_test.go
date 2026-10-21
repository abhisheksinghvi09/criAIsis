package llm_test

import (
	"context"
	"testing"

	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/llm"
)

// The simulator relies on FakeEmbedder ranking lexically related runbooks above
// unrelated ones. If that stops holding, offline retrieval assertions are meaningless.
func TestFakeEmbedder_RanksLexicalOverlap(t *testing.T) {
	texts := []string{
		"remaining connection slots are reserved for superuser connections",
		"pg_stat_activity shows idle in transaction connection slots exhausted",
		"traceroute shows packet loss between availability zones",
	}

	vecs, err := llm.FakeEmbedder{}.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("embedding failed: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("expected %d vectors, got %d", len(texts), len(vecs))
	}
	for i, v := range vecs {
		if len(v) != value.ExpectedEmbeddingDimensions {
			t.Fatalf("vector %d has %d dims", i, len(v))
		}
	}

	related, err := vecs[0].CosineSimilarity(vecs[1])
	if err != nil {
		t.Fatalf("similarity failed: %v", err)
	}
	unrelated, err := vecs[0].CosineSimilarity(vecs[2])
	if err != nil {
		t.Fatalf("similarity failed: %v", err)
	}
	if related <= unrelated {
		t.Errorf("expected related texts to score higher: related=%f unrelated=%f", related, unrelated)
	}
}

func TestFakeEmbedder_IsDeterministic(t *testing.T) {
	ctx := context.Background()
	first, err := llm.FakeEmbedder{}.Embed(ctx, []string{"connection pool saturation"})
	if err != nil {
		t.Fatalf("embedding failed: %v", err)
	}
	second, err := llm.FakeEmbedder{}.Embed(ctx, []string{"connection pool saturation"})
	if err != nil {
		t.Fatalf("embedding failed: %v", err)
	}
	for i := range first[0] {
		if first[0][i] != second[0][i] {
			t.Fatalf("embedding is not deterministic at index %d", i)
		}
	}
}
