// Package llm wraps the chat and embedding providers behind narrow interfaces so the
// clash orchestrator never depends on a concrete vendor SDK.
//
// Chat and embeddings are deliberately separate contracts: Anthropic exposes no
// embeddings endpoint, so the two halves are always served by different vendors.
package llm

import (
	"context"

	"criaisis/internal/domain/value"
)

// Role selects which configured model and effort tier serves a request.
type Role string

const (
	// RoleSpecialist serves Stage 1 hypotheses, which run four-wide under a <10s budget.
	RoleSpecialist Role = "specialist"

	// RoleSynthesis serves Stage 2 consensus, which trades latency for reasoning depth.
	RoleSynthesis Role = "synthesis"
)

// ChatRequest is one grounded completion. When Schema is set the provider must
// return JSON conforming to it, removing prose parsing from the orchestrator.
type ChatRequest struct {
	Role      Role
	System    string
	User      string
	Schema    map[string]any
	MaxTokens int64
}

// ChatProvider generates specialist hypotheses and consensus synthesis.
type ChatProvider interface {
	Complete(ctx context.Context, req ChatRequest) (string, error)
}

// EmbeddingProvider converts runbook chunks and incident queries into vectors.
// Implementations must return vectors of exactly value.ExpectedEmbeddingDimensions.
type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([]value.EmbeddingVector, error)
}
