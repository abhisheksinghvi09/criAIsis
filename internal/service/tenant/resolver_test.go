package tenant_test

import (
	"context"
	"errors"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/notify"
	"criaisis/internal/service/tenant"
)

const testCipherKey = "01234567890123456789012345678901" // exactly 32 bytes

func newCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.New(testCipherKey)
	if err != nil {
		t.Fatalf("building cipher: %v", err)
	}
	return c
}

// mockSettingsRepo is an in-memory stand-in keyed by workspace id.
type mockSettingsRepo struct {
	byWorkspace map[value.WorkspaceID]*entity.WorkspaceSettings
	getErr      error
}

func newMockSettingsRepo() *mockSettingsRepo {
	return &mockSettingsRepo{byWorkspace: map[value.WorkspaceID]*entity.WorkspaceSettings{}}
}

func (m *mockSettingsRepo) Create(_ context.Context, s *entity.WorkspaceSettings) error {
	m.byWorkspace[s.WorkspaceID()] = s
	return nil
}

func (m *mockSettingsRepo) Get(_ context.Context, wsID value.WorkspaceID) (*entity.WorkspaceSettings, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	s, ok := m.byWorkspace[wsID]
	if !ok {
		return nil, errors.New("settings not found")
	}
	return s, nil
}

func (m *mockSettingsRepo) Update(_ context.Context, s *entity.WorkspaceSettings) error {
	m.byWorkspace[s.WorkspaceID()] = s
	return nil
}

// fullyConfiguredSettings builds settings with real, decryptable LLM and
// embedding keys sealed under cipher, and a valid Slack notification target.
func fullyConfiguredSettings(t *testing.T, cipher *crypto.Cipher, wsID value.WorkspaceID) *entity.WorkspaceSettings {
	t.Helper()
	s, err := entity.NewWorkspaceSettings(wsID, entity.SettingsDefaults{})
	if err != nil {
		t.Fatalf("building settings: %v", err)
	}

	llmKey, err := cipher.Encrypt("sk-ant-tenant-key")
	if err != nil {
		t.Fatalf("sealing llm key: %v", err)
	}
	if err := s.SetLLM("anthropic", llmKey, "claude-opus-5", "claude-opus-5"); err != nil {
		t.Fatalf("SetLLM: %v", err)
	}

	embedKey, err := cipher.Encrypt("sk-embed-tenant-key")
	if err != nil {
		t.Fatalf("sealing embedding key: %v", err)
	}
	if err := s.SetEmbeddings("openai", embedKey, "text-embedding-3-small", "https://api.openai.com/v1"); err != nil {
		t.Fatalf("SetEmbeddings: %v", err)
	}

	webhook, err := cipher.Encrypt("https://hooks.slack.com/services/T/B/x")
	if err != nil {
		t.Fatalf("sealing webhook url: %v", err)
	}
	if err := s.SetNotificationTarget("slack", webhook); err != nil {
		t.Fatalf("SetNotificationTarget: %v", err)
	}

	return s
}

func TestResolve_BuildsRuntimeFromDecryptedCredentials(t *testing.T) {
	cipher := newCipher(t)
	repo := newMockSettingsRepo()
	wsID := value.NewWorkspaceID()
	settings := fullyConfiguredSettings(t, cipher, wsID)
	if err := repo.Create(context.Background(), settings); err != nil {
		t.Fatalf("seeding settings: %v", err)
	}

	resolver := tenant.NewResolver(repo, cipher)
	rt, err := resolver.Resolve(context.Background(), wsID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rt.Chat == nil || rt.Embedder == nil || rt.Notifier == nil {
		t.Fatal("expected every client on the runtime to be built")
	}
	if rt.Settings.WorkspaceID() != wsID {
		t.Error("runtime settings do not match the resolved workspace")
	}
}

func TestResolve_ReturnsErrNotConfiguredWhenCredentialsMissing(t *testing.T) {
	cipher := newCipher(t)
	repo := newMockSettingsRepo()
	wsID := value.NewWorkspaceID()
	settings, err := entity.NewWorkspaceSettings(wsID, entity.SettingsDefaults{})
	if err != nil {
		t.Fatalf("building settings: %v", err)
	}
	_ = repo.Create(context.Background(), settings)

	resolver := tenant.NewResolver(repo, cipher)
	if _, err := resolver.Resolve(context.Background(), wsID); !errors.Is(err, tenant.ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestResolve_PropagatesSettingsLookupFailure(t *testing.T) {
	cipher := newCipher(t)
	repo := newMockSettingsRepo()
	resolver := tenant.NewResolver(repo, cipher)

	if _, err := resolver.Resolve(context.Background(), value.NewWorkspaceID()); err == nil {
		t.Error("expected an error for an unknown workspace")
	}
}

// A ciphertext sealed under one key can never be decrypted correctly under
// another: this is the scenario a rotated CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY
// produces for every tenant credential stored under the old one.
func TestResolve_FailsClosedOnUndecryptableCredential(t *testing.T) {
	sealingCipher := newCipher(t)
	wsID := value.NewWorkspaceID()
	repo := newMockSettingsRepo()
	settings := fullyConfiguredSettings(t, sealingCipher, wsID)
	_ = repo.Create(context.Background(), settings)

	otherKey := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	wrongCipher, err := crypto.New(otherKey)
	if err != nil {
		t.Fatalf("building second cipher: %v", err)
	}

	resolver := tenant.NewResolver(repo, wrongCipher)
	if _, err := resolver.Resolve(context.Background(), wsID); err == nil {
		t.Error("expected decryption under the wrong key to fail")
	}
}

func TestResolveNotifier_NoneYieldsDiscardWithoutTouchingCiphertext(t *testing.T) {
	cipher := newCipher(t)
	repo := newMockSettingsRepo()
	wsID := value.NewWorkspaceID()
	settings, err := entity.NewWorkspaceSettings(wsID, entity.SettingsDefaults{})
	if err != nil {
		t.Fatalf("building settings: %v", err)
	}
	_ = repo.Create(context.Background(), settings)

	resolver := tenant.NewResolver(repo, cipher)
	notifier, err := resolver.ResolveNotifier(context.Background(), wsID)
	if err != nil {
		t.Fatalf("ResolveNotifier: %v", err)
	}
	if _, ok := notifier.(notify.Discard); !ok {
		t.Errorf("expected a Discard notifier for an unconfigured destination, got %T", notifier)
	}
}

func TestResolveNotifier_BuildsConfiguredDestination(t *testing.T) {
	cipher := newCipher(t)
	wsID := value.NewWorkspaceID()
	repo := newMockSettingsRepo()
	settings := fullyConfiguredSettings(t, cipher, wsID)
	_ = repo.Create(context.Background(), settings)

	resolver := tenant.NewResolver(repo, cipher)
	notifier, err := resolver.ResolveNotifier(context.Background(), wsID)
	if err != nil {
		t.Fatalf("ResolveNotifier: %v", err)
	}
	if _, ok := notifier.(notify.Discard); ok {
		t.Error("expected a real notifier, got Discard")
	}
}

func TestResolveEmbedder_RequiresEmbeddingKeyEvenWithoutLLMKey(t *testing.T) {
	cipher := newCipher(t)
	wsID := value.NewWorkspaceID()
	repo := newMockSettingsRepo()
	settings, err := entity.NewWorkspaceSettings(wsID, entity.SettingsDefaults{})
	if err != nil {
		t.Fatalf("building settings: %v", err)
	}
	_ = repo.Create(context.Background(), settings)

	resolver := tenant.NewResolver(repo, cipher)
	if _, err := resolver.ResolveEmbedder(context.Background(), wsID); !errors.Is(err, tenant.ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestResolveEmbedder_SucceedsWithOnlyAnEmbeddingKey(t *testing.T) {
	cipher := newCipher(t)
	wsID := value.NewWorkspaceID()
	repo := newMockSettingsRepo()
	settings, err := entity.NewWorkspaceSettings(wsID, entity.SettingsDefaults{})
	if err != nil {
		t.Fatalf("building settings: %v", err)
	}
	embedKey, err := cipher.Encrypt("sk-embed-tenant-key")
	if err != nil {
		t.Fatalf("sealing embedding key: %v", err)
	}
	if err := settings.SetEmbeddings("openai", embedKey, "text-embedding-3-small", "https://api.openai.com/v1"); err != nil {
		t.Fatalf("SetEmbeddings: %v", err)
	}
	_ = repo.Create(context.Background(), settings)

	resolver := tenant.NewResolver(repo, cipher)
	embedder, err := resolver.ResolveEmbedder(context.Background(), wsID)
	if err != nil {
		t.Fatalf("ResolveEmbedder: %v", err)
	}
	if embedder == nil {
		t.Error("expected a non-nil embedder")
	}
}
