package llm

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"

	"criaisis/internal/domain/value"
)

// ChatFunc adapts a plain function to ChatProvider so callers can supply their own
// deterministic stub next to the schemas it has to satisfy.
type ChatFunc func(ctx context.Context, req ChatRequest) (string, error)

// Complete implements ChatProvider.
func (f ChatFunc) Complete(ctx context.Context, req ChatRequest) (string, error) { return f(ctx, req) }

// FakeEmbedder produces deterministic vectors locally so the simulation framework and
// repository tests can exercise hybrid search without network calls or API spend.
//
// ponytail: hashing-trick bag of words, so it models lexical overlap only and has no
// semantic understanding. Swap in OpenAIEmbedder whenever retrieval quality is what
// is under test.
type FakeEmbedder struct{}

var _ EmbeddingProvider = (*FakeEmbedder)(nil)

// Embed hashes each token into a fixed bucket and L2-normalizes the result, which
// makes cosine similarity track lexical overlap between texts.
func (FakeEmbedder) Embed(_ context.Context, texts []string) ([]value.EmbeddingVector, error) {
	vectors := make([]value.EmbeddingVector, 0, len(texts))
	for _, text := range texts {
		vec, err := value.NewEmbeddingVector(hashVector(text))
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, vec)
	}
	return vectors, nil
}

// hashVector buckets the tokens of text into a unit vector of the expected dimension.
func hashVector(text string) []float32 {
	buckets := make([]float32, value.ExpectedEmbeddingDimensions)
	for _, token := range tokenize(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(token))
		buckets[h.Sum32()%value.ExpectedEmbeddingDimensions]++
	}
	return normalize(buckets)
}

// tokenize lowercases and splits on any non-alphanumeric rune.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// normalize scales a vector to unit length, leaving an all-zero vector untouched.
func normalize(vec []float32) []float32 {
	var sumSquares float64
	for _, v := range vec {
		sumSquares += float64(v) * float64(v)
	}
	if sumSquares == 0 {
		return vec
	}
	norm := float32(math.Sqrt(sumSquares))
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}
