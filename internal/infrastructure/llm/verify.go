package llm

import (
	"context"
	"fmt"
	"time"

	"criaisis/internal/domain/value"
)

// verifyTimeout bounds a credential check. This runs while a customer waits on an
// HTTP response, so it fails fast rather than hanging their setup.
const verifyTimeout = 20 * time.Second

// VerifyAnthropic proves a tenant's model credential works before it is stored.
// Rejecting a bad key at setup is far cheaper than discovering it four model
// calls into a live incident.
func VerifyAnthropic(ctx context.Context, opts AnthropicOptions) error {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	_, err := NewAnthropic(opts).Complete(ctx, ChatRequest{
		Role:      RoleSpecialist,
		System:    "You are validating an API credential.",
		User:      "Reply with the single word: ok",
		MaxTokens: 16,
	})
	if err != nil {
		return fmt.Errorf("model %q did not respond: %w", opts.SpecialistModel, err)
	}
	return nil
}

// VerifyEmbeddings proves a tenant's retrieval credential works and returns
// vectors of the dimension the pgvector column requires. A model with the wrong
// dimension would fail on every insert, so it is caught here.
func VerifyEmbeddings(ctx context.Context, opts EmbeddingOptions) error {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	vectors, err := NewOpenAIEmbedder(opts).Embed(ctx, []string{"criaisis credential check"})
	if err != nil {
		return fmt.Errorf("embedding model %q did not respond: %w", opts.Model, err)
	}
	if len(vectors) != 1 {
		return fmt.Errorf("embedding model %q returned %d vectors for one input", opts.Model, len(vectors))
	}
	if len(vectors[0]) != value.ExpectedEmbeddingDimensions {
		return fmt.Errorf("embedding model %q returns %d dimensions, but the schema requires %d",
			opts.Model, len(vectors[0]), value.ExpectedEmbeddingDimensions)
	}
	return nil
}
