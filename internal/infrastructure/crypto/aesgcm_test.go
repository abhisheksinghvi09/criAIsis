package crypto_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"criaisis/internal/infrastructure/crypto"
)

const keyA = "01234567890123456789012345678901"
const keyB = "abcdefghijabcdefghijabcdefghijab"

func must(c *crypto.Cipher, err error) *crypto.Cipher {
	if err != nil {
		panic(err)
	}
	return c
}

func TestCipher_RoundTripWithBinding(t *testing.T) {
	c := must(crypto.New(keyA))
	sealed, err := c.Seal("xoxb-secret", "bind-1")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("xoxb-secret")) {
		t.Fatal("plaintext visible in ciphertext")
	}
	got, err := c.Open(sealed, "bind-1")
	if err != nil || got != "xoxb-secret" {
		t.Fatalf("Open = %q, %v", got, err)
	}
}

// A value moved to another tenant's row or another column must not decrypt.
func TestCipher_RejectsAValueMovedToAnotherBinding(t *testing.T) {
	c := must(crypto.New(keyA))
	sealed, _ := c.Seal("sk-key", crypto.SettingAAD("ws-A", crypto.FieldLLMKey))
	for _, aad := range []string{
		crypto.SettingAAD("ws-B", crypto.FieldLLMKey),
		crypto.SettingAAD("ws-A", crypto.FieldEmbeddingKey),
		"",
	} {
		if _, err := c.Open(sealed, aad); err == nil {
			t.Errorf("opened under binding %q", aad)
		}
	}
}

func TestCipher_UsesAFreshNonce(t *testing.T) {
	c := must(crypto.New(keyA))
	a, _ := c.Seal("same", "x")
	b, _ := c.Seal("same", "x")
	if bytes.Equal(a, b) {
		t.Error("two seals of the same value are identical")
	}
}

func TestCipher_RejectsTamperingAndTheWrongKey(t *testing.T) {
	c := must(crypto.New(keyA))
	sealed, _ := c.Seal("secret", "x")
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := c.Open(tampered, "x"); err == nil {
		t.Error("tampered value opened")
	}
	other := must(crypto.New(keyB))
	if _, err := other.Open(sealed, "x"); err == nil {
		t.Error("value opened with the wrong key")
	}
}

// Rotation: the new key seals, the previous key still opens old values, and
// those values are reported for rewrap.
func TestKeyring_RotatesWithoutLosingOldValues(t *testing.T) {
	old := must(crypto.New(keyA))
	oldSealed, _ := old.Seal("v1-secret", "x")

	ring := must(crypto.NewKeyring(2, keyB, map[int]string{1: keyA}))
	if got, err := ring.Open(oldSealed, "x"); err != nil || got != "v1-secret" {
		t.Fatalf("previous-key value did not open: %q %v", got, err)
	}
	if !ring.NeedsRewrap(oldSealed) {
		t.Error("value under a previous key not flagged for rewrap")
	}
	fresh, _ := ring.Seal("v2-secret", "x")
	if ring.NeedsRewrap(fresh) {
		t.Error("freshly sealed value flagged for rewrap")
	}
	// Once the old key is retired, values sealed under the new one still open.
	newOnly := must(crypto.NewKeyring(2, keyB, nil))
	if _, err := newOnly.Open(fresh, "x"); err != nil {
		t.Errorf("current-key value failed after retiring the old key: %v", err)
	}
	if _, err := newOnly.Open(oldSealed, "x"); err == nil {
		t.Error("retired key's value still opened")
	}
}

// Values written before the envelope (nonce|ciphertext, no binding) must keep working.
func TestCipher_OpensLegacyValues(t *testing.T) {
	block, _ := aes.NewCipher([]byte(keyA))
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	legacy := aead.Seal(append([]byte(nil), nonce...), nonce, []byte("old-token"), nil)

	c := must(crypto.New(keyA))
	got, err := c.Open(legacy, "any-binding")
	if err != nil || got != "old-token" {
		t.Fatalf("legacy value: %q %v", got, err)
	}
	if !c.NeedsRewrap(legacy) {
		t.Error("legacy value not flagged for rewrap")
	}
}

func TestParseKey_AcceptsRawAndBase64AndRejectsOthers(t *testing.T) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	if k, err := crypto.ParseKey("base64:" + base64.StdEncoding.EncodeToString(raw)); err != nil || !bytes.Equal(k, raw) {
		t.Errorf("base64 key: %v", err)
	}
	if _, err := crypto.ParseKey(keyA); err != nil {
		t.Errorf("raw key: %v", err)
	}
	for _, bad := range []string{"short", "base64:" + base64.StdEncoding.EncodeToString(raw[:16]), "base64:!!"} {
		if _, err := crypto.ParseKey(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := crypto.NewKeyring(1, keyA, map[int]string{1: keyB}); err == nil {
		t.Error("duplicate key id accepted")
	}
}

func TestCipher_EmptyStaysEmpty(t *testing.T) {
	c := must(crypto.New(keyA))
	if s, _ := c.Seal("", "x"); s != nil {
		t.Error("empty plaintext sealed to bytes")
	}
	if p, err := c.Open(nil, "x"); err != nil || p != "" {
		t.Error("empty value did not open to empty")
	}
}
