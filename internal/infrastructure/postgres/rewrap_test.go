package postgres_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/postgres"

	"github.com/google/uuid"
)

const oldKey = "01234567890123456789012345678901"
const newKey = "abcdefghijabcdefghijabcdefghijab"

// legacySeal produces a pre-envelope value: nonce|ciphertext, no binding.
func legacySeal(t *testing.T, plain string) []byte {
	t.Helper()
	block, _ := aes.NewCipher([]byte(oldKey))
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	return aead.Seal(append([]byte(nil), nonce...), nonce, []byte(plain), nil)
}

// Rotation end to end: legacy and old-key values are re-sealed under the new key
// with their bindings, the old key can then be retired, and a rerun is a no-op.
func TestRewrapCredentials_RotatesEveryStoredSecret(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()

	team := "T_REWRAP_" + uuid.NewString()[:6]
	ws, err := entity.NewWorkspace(team, "Rewrap", legacySeal(t, "xoxb-legacy"))
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewWorkspaceRepository(pool).Create(ctx, ws); err != nil {
		t.Fatal(err)
	}

	oldRing, _ := crypto.New(oldKey)
	viaOldKey, _ := oldRing.Seal("sk-embed", crypto.SettingAAD(ws.ID().String(), crypto.FieldEmbeddingKey))
	settings, _ := entity.NewWorkspaceSettings(ws.ID(), entity.SettingsDefaults{})
	_ = settings.SetLLM("anthropic", legacySeal(t, "sk-ant"), "", "")
	_ = settings.SetEmbeddings("openai", viaOldKey, "", "")
	if err := postgres.NewSettingsRepository(pool).Create(ctx, settings); err != nil {
		t.Fatal(err)
	}

	ring, err := crypto.NewKeyring(2, newKey, map[int]string{1: oldKey})
	if err != nil {
		t.Fatal(err)
	}
	n, err := postgres.RewrapCredentials(ctx, pool, ring)
	if err != nil {
		t.Fatalf("RewrapCredentials: %v", err)
	}
	if n != 3 {
		t.Errorf("rewrapped %d values, want 3", n)
	}
	if again, err := postgres.RewrapCredentials(ctx, pool, ring); err != nil || again != 0 {
		t.Errorf("second run rewrapped %d (err %v), want 0", again, err)
	}

	// The old key is now unnecessary: open everything with the new key alone.
	newOnly, _ := crypto.NewKeyring(2, newKey, nil)
	got, _ := postgres.NewWorkspaceRepository(pool).GetByID(ctx, ws.ID())
	if tok, err := newOnly.Open(got.SlackBotTokenEncrypted(), crypto.BotTokenAAD(team)); err != nil || tok != "xoxb-legacy" {
		t.Errorf("bot token after rotation: %q %v", tok, err)
	}
	s, _ := postgres.NewSettingsRepository(pool).Get(ctx, ws.ID())
	if v, err := newOnly.Open(s.LLMAPIKeyEncrypted(), crypto.SettingAAD(ws.ID().String(), crypto.FieldLLMKey)); err != nil || v != "sk-ant" {
		t.Errorf("llm key after rotation: %q %v", v, err)
	}
	if v, err := newOnly.Open(s.EmbeddingAPIKeyEncrypted(), crypto.SettingAAD(ws.ID().String(), crypto.FieldEmbeddingKey)); err != nil || v != "sk-embed" {
		t.Errorf("embedding key after rotation: %q %v", v, err)
	}
}
