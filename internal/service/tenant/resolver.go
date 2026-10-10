// Package tenant turns one customer's stored credentials into the live clients
// their investigation runs on.
//
// Every model call and every notification in criAIsis is made with the tenant's
// own key. There is no platform-wide fallback key on purpose: one tenant can
// never spend, rate-limit or leak through another's credential.
package tenant

import (
	"context"
	"errors"
	"fmt"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/llm"
	"criaisis/internal/infrastructure/notify"
)

// ErrNotConfigured means the tenant has not supplied the credentials an
// investigation requires. It is distinguished from a transient failure so callers
// can tell the customer to finish setup rather than retrying forever.
var ErrNotConfigured = errors.New("workspace is not fully configured")

// Runtime is one tenant's live integration clients.
type Runtime struct {
	Chat     llm.ChatProvider
	Embedder llm.EmbeddingProvider
	Notifier notify.Notifier
	Settings *entity.WorkspaceSettings
}

// Resolver builds a Runtime from stored, encrypted credentials.
type Resolver struct {
	settings repository.SettingsRepository
	cipher   *crypto.Cipher
}

// NewResolver wires the resolver.
func NewResolver(settings repository.SettingsRepository, cipher *crypto.Cipher) *Resolver {
	return &Resolver{settings: settings, cipher: cipher}
}

// Resolve decrypts a tenant's credentials and constructs their clients.
//
// ponytail: clients are rebuilt per investigation rather than cached. Construction
// is just an HTTP client plus a key, and rebuilding means a rotated credential
// takes effect on the next incident with no cache invalidation to get wrong.
func (r *Resolver) Resolve(ctx context.Context, wsID value.WorkspaceID) (*Runtime, error) {
	settings, err := r.settings.Get(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("loading workspace settings: %w", err)
	}
	if !settings.IsReadyToInvestigate() {
		return nil, fmt.Errorf("%w: missing %v", ErrNotConfigured, settings.MissingRequirements())
	}

	chat, err := r.chatFor(settings)
	if err != nil {
		return nil, err
	}
	embedder, err := r.embedderFor(settings)
	if err != nil {
		return nil, err
	}
	notifier, err := r.notifierFor(settings)
	if err != nil {
		return nil, err
	}

	return &Runtime{Chat: chat, Embedder: embedder, Notifier: notifier, Settings: settings}, nil
}

// ResolveNotifier builds only the delivery client, for callers that need to reach
// the tenant's channel without running a debate.
func (r *Resolver) ResolveNotifier(ctx context.Context, wsID value.WorkspaceID) (notify.Notifier, error) {
	settings, err := r.settings.Get(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("loading workspace settings: %w", err)
	}
	return r.notifierFor(settings)
}

// ResolveEmbedder builds only the retrieval client, for ingestion, which needs no
// debate model and must work before the tenant has finished full setup.
func (r *Resolver) ResolveEmbedder(ctx context.Context, wsID value.WorkspaceID) (llm.EmbeddingProvider, error) {
	settings, err := r.settings.Get(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("loading workspace settings: %w", err)
	}
	if !settings.HasEmbeddingKey() {
		return nil, fmt.Errorf("%w: missing embedding api key", ErrNotConfigured)
	}
	return r.embedderFor(settings)
}

// chatFor builds the tenant's debate client from their own key.
func (r *Resolver) chatFor(s *entity.WorkspaceSettings) (llm.ChatProvider, error) {
	key, err := r.cipher.Decrypt(s.LLMAPIKeyEncrypted())
	if err != nil {
		return nil, fmt.Errorf("decrypting model api key: %w", err)
	}
	return llm.NewAnthropic(llm.AnthropicOptions{
		APIKey:          key,
		SpecialistModel: s.SpecialistModel(),
		SynthesisModel:  s.SynthesisModel(),
	}), nil
}

// embedderFor builds the tenant's retrieval client from their own key.
func (r *Resolver) embedderFor(s *entity.WorkspaceSettings) (llm.EmbeddingProvider, error) {
	key, err := r.cipher.Decrypt(s.EmbeddingAPIKeyEncrypted())
	if err != nil {
		return nil, fmt.Errorf("decrypting embedding api key: %w", err)
	}
	return llm.NewOpenAIEmbedder(llm.EmbeddingOptions{
		APIKey:  key,
		Model:   s.EmbeddingModel(),
		BaseURL: s.EmbeddingBaseURL(),
	}), nil
}

// notifierFor builds the tenant's delivery client, or a discard when they have
// configured no destination.
func (r *Resolver) notifierFor(s *entity.WorkspaceSettings) (notify.Notifier, error) {
	provider, err := notify.ParseProvider(s.NotifyProvider())
	if err != nil {
		return nil, err
	}
	if provider == notify.ProviderNone {
		return notify.Discard{}, nil
	}

	webhookURL, err := r.cipher.Decrypt(s.NotifyWebhookEncrypted())
	if err != nil {
		return nil, fmt.Errorf("decrypting webhook url: %w", err)
	}
	return notify.New(provider, webhookURL)
}
