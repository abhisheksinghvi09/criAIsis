package value

import (
	"fmt"
	"math"
)

const ExpectedEmbeddingDimensions = 1536

// EmbeddingVector models a high-dimensional dense vector representing semantic content.
// We strictly enforce 1536 dimensions corresponding to standard embedding models (e.g. text-embedding-3-small).
type EmbeddingVector []float32

// NewEmbeddingVector constructs and validates embedding dimensions to prevent runtime DB vector errors.
func NewEmbeddingVector(values []float32) (EmbeddingVector, error) {
	if len(values) != ExpectedEmbeddingDimensions {
		return nil, fmt.Errorf("invalid embedding dimensions: expected %d, got %d", ExpectedEmbeddingDimensions, len(values))
	}
	return EmbeddingVector(values), nil
}

// Slice returns the underlying float32 slice for pgvector driver conversions.
func (e EmbeddingVector) Slice() []float32 {
	return []float32(e)
}

// CosineSimilarity provides a domain-level comparison useful for unit tests and in-memory reranking.
func (e EmbeddingVector) CosineSimilarity(other EmbeddingVector) (float32, error) {
	if len(e) != len(other) {
		return 0, fmt.Errorf("vector length mismatch: %d vs %d", len(e), len(other))
	}

	var dotProduct, normA, normB float64
	for i := range e {
		valA := float64(e[i])
		valB := float64(other[i])
		dotProduct += valA * valB
		normA += valA * valA
		normB += valB * valB
	}

	if normA == 0 || normB == 0 {
		return 0, nil
	}

	similarity := dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
	return float32(similarity), nil
}
