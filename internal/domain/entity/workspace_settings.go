package entity

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"criaisis/internal/domain/value"
)

// WorkspaceSettings holds one tenant's own integration credentials: the models
// their debates run on and the channel their investigations are posted to.
//
// Secrets are held only as ciphertext. The domain layer never sees a plaintext
// key, so an accidental log of this entity cannot leak one.
type WorkspaceSettings struct {
	workspaceID value.WorkspaceID

	llmProvider        string
	llmAPIKeyEncrypted []byte
	specialistModel    string
	synthesisModel     string

	embeddingProvider        string
	embeddingAPIKeyEncrypted []byte
	embeddingModel           string
	embeddingBaseURL         string

	notifyProvider         string
	notifyWebhookEncrypted []byte

	createdAt time.Time
	updatedAt time.Time
}

// SettingsDefaults are the platform's starting values for a new tenant. They carry
// model names and endpoints only: never a key, because keys are the tenant's.
type SettingsDefaults struct {
	SpecialistModel  string
	SynthesisModel   string
	EmbeddingModel   string
	EmbeddingBaseURL string
}

// NewWorkspaceSettings creates an unconfigured settings record. A tenant starts
// with no credentials and no destination, and must supply both before a debate
// can run for them.
func NewWorkspaceSettings(workspaceID value.WorkspaceID, defaults SettingsDefaults) (*WorkspaceSettings, error) {
	if workspaceID.IsZero() {
		return nil, errors.New("workspace id cannot be zero")
	}

	now := time.Now().UTC()
	return &WorkspaceSettings{
		workspaceID:       workspaceID,
		llmProvider:       "anthropic",
		specialistModel:   orDefault(defaults.SpecialistModel, "claude-opus-5"),
		synthesisModel:    orDefault(defaults.SynthesisModel, "claude-opus-5"),
		embeddingProvider: "openai",
		embeddingModel:    orDefault(defaults.EmbeddingModel, "text-embedding-3-small"),
		embeddingBaseURL:  orDefault(defaults.EmbeddingBaseURL, "https://api.openai.com/v1"),
		notifyProvider:    "none",
		createdAt:         now,
		updatedAt:         now,
	}, nil
}

// ReconstituteWorkspaceSettings rebuilds the entity from storage.
func ReconstituteWorkspaceSettings(
	workspaceID value.WorkspaceID,
	llmProvider string, llmKey []byte, specialistModel, synthesisModel string,
	embeddingProvider string, embeddingKey []byte, embeddingModel, embeddingBaseURL string,
	notifyProvider string, notifyWebhook []byte,
	createdAt, updatedAt time.Time,
) *WorkspaceSettings {
	return &WorkspaceSettings{
		workspaceID:              workspaceID,
		llmProvider:              llmProvider,
		llmAPIKeyEncrypted:       llmKey,
		specialistModel:          specialistModel,
		synthesisModel:           synthesisModel,
		embeddingProvider:        embeddingProvider,
		embeddingAPIKeyEncrypted: embeddingKey,
		embeddingModel:           embeddingModel,
		embeddingBaseURL:         embeddingBaseURL,
		notifyProvider:           notifyProvider,
		notifyWebhookEncrypted:   notifyWebhook,
		createdAt:                createdAt,
		updatedAt:                updatedAt,
	}
}

func (s *WorkspaceSettings) WorkspaceID() value.WorkspaceID   { return s.workspaceID }
func (s *WorkspaceSettings) LLMProvider() string              { return s.llmProvider }
func (s *WorkspaceSettings) LLMAPIKeyEncrypted() []byte       { return s.llmAPIKeyEncrypted }
func (s *WorkspaceSettings) SpecialistModel() string          { return s.specialistModel }
func (s *WorkspaceSettings) SynthesisModel() string           { return s.synthesisModel }
func (s *WorkspaceSettings) EmbeddingProvider() string        { return s.embeddingProvider }
func (s *WorkspaceSettings) EmbeddingAPIKeyEncrypted() []byte { return s.embeddingAPIKeyEncrypted }
func (s *WorkspaceSettings) EmbeddingModel() string           { return s.embeddingModel }
func (s *WorkspaceSettings) EmbeddingBaseURL() string         { return s.embeddingBaseURL }
func (s *WorkspaceSettings) NotifyProvider() string           { return s.notifyProvider }
func (s *WorkspaceSettings) NotifyWebhookEncrypted() []byte   { return s.notifyWebhookEncrypted }
func (s *WorkspaceSettings) CreatedAt() time.Time             { return s.createdAt }
func (s *WorkspaceSettings) UpdatedAt() time.Time             { return s.updatedAt }

// HasLLMKey reports whether the tenant has supplied a model credential.
func (s *WorkspaceSettings) HasLLMKey() bool { return len(s.llmAPIKeyEncrypted) > 0 }

// HasEmbeddingKey reports whether the tenant has supplied an embeddings credential.
func (s *WorkspaceSettings) HasEmbeddingKey() bool { return len(s.embeddingAPIKeyEncrypted) > 0 }

// IsReadyToInvestigate reports whether a debate can run for this tenant at all.
// Both credentials are required: without embeddings there is no retrieval, and
// without a model there is no debate.
func (s *WorkspaceSettings) IsReadyToInvestigate() bool {
	return s.HasLLMKey() && s.HasEmbeddingKey()
}

// MissingRequirements names what the tenant still has to configure, so the API can
// tell them precisely rather than failing later inside a worker.
func (s *WorkspaceSettings) MissingRequirements() []string {
	var missing []string
	if !s.HasLLMKey() {
		missing = append(missing, "model api key")
	}
	if !s.HasEmbeddingKey() {
		missing = append(missing, "embedding api key")
	}
	if s.notifyProvider == "none" {
		missing = append(missing, "notification destination")
	}
	return missing
}

// SetLLM records the tenant's model credential and model choices.
func (s *WorkspaceSettings) SetLLM(provider string, encryptedKey []byte, specialistModel, synthesisModel string) error {
	if provider != "anthropic" {
		return fmt.Errorf("unsupported model provider %q: only anthropic is supported", provider)
	}
	if len(encryptedKey) == 0 {
		return errors.New("model api key is required")
	}

	s.llmProvider = provider
	s.llmAPIKeyEncrypted = encryptedKey
	s.specialistModel = orDefault(specialistModel, s.specialistModel)
	s.synthesisModel = orDefault(synthesisModel, s.synthesisModel)
	s.updatedAt = time.Now().UTC()
	return nil
}

// SetEmbeddings records the tenant's retrieval credential and endpoint.
func (s *WorkspaceSettings) SetEmbeddings(provider string, encryptedKey []byte, model, baseURL string) error {
	if provider != "openai" {
		return fmt.Errorf("unsupported embedding provider %q: only openai-compatible endpoints are supported", provider)
	}
	if len(encryptedKey) == 0 {
		return errors.New("embedding api key is required")
	}

	s.embeddingProvider = provider
	s.embeddingAPIKeyEncrypted = encryptedKey
	s.embeddingModel = orDefault(model, s.embeddingModel)
	s.embeddingBaseURL = orDefault(baseURL, s.embeddingBaseURL)
	s.updatedAt = time.Now().UTC()
	return nil
}

// SetNotificationTarget records where this tenant's investigations are posted.
func (s *WorkspaceSettings) SetNotificationTarget(provider string, encryptedURL []byte) error {
	switch provider {
	case "none", "slack", "discord":
	default:
		return fmt.Errorf("unsupported notification provider %q", provider)
	}
	if provider != "none" && len(encryptedURL) == 0 {
		return fmt.Errorf("provider %q requires a webhook url", provider)
	}

	s.notifyProvider = provider
	s.notifyWebhookEncrypted = encryptedURL
	s.updatedAt = time.Now().UTC()
	return nil
}

// orDefault keeps an existing value when the caller supplies nothing.
func orDefault(candidate, fallback string) string {
	if trimmed := strings.TrimSpace(candidate); trimmed != "" {
		return trimmed
	}
	return fallback
}
