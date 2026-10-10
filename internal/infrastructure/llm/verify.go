package llm

import (
	"context"
	"fmt"
	"net"
	"net/url"
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

	if err := validateEmbeddingBaseURL(ctx, opts.BaseURL); err != nil {
		return err
	}

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

// validateEmbeddingBaseURL guards the one tenant-controlled endpoint this platform
// calls out to. criAIsis is bring-your-own-embeddings, so the host can't be
// allowlisted the way the fixed Slack/Discord webhook hosts are — instead it must
// not resolve to a loopback, link-local (this includes the 169.254.169.254 cloud
// metadata address), or private address, or a tenant could use their own setup
// form to make the platform probe its own internal network and read the result
// back from the verification error.
//
// ponytail: resolves once, at the point this URL is saved, not per later call —
// a DNS answer that changes after that (rebinding) is not re-checked. Upgrade to
// a custom Dialer that pins the validated IP if that gap ever matters for this
// threat model.
func validateEmbeddingBaseURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("embedding base_url is not a valid url: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("embedding base_url must use https, got %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("embedding base_url has no host")
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("embedding base_url host %q did not resolve: %w", host, err)
	}
	for _, addr := range addrs {
		ip := addr.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
			return fmt.Errorf("embedding base_url host %q resolves to a non-routable address", host)
		}
	}
	return nil
}
