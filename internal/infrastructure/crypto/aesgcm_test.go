package crypto_test

import (
	"strings"
	"testing"

	"criaisis/internal/infrastructure/crypto"
)

const testKey = "01234567890123456789012345678901"

func TestCipher_RoundTrip(t *testing.T) {
	c, err := crypto.New(testKey)
	if err != nil {
		t.Fatalf("building cipher: %v", err)
	}

	secret := "xoxb-not-a-real-slack-token"
	sealed, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	if strings.Contains(string(sealed), secret) {
		t.Fatal("the plaintext is visible in the ciphertext")
	}

	opened, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatalf("decrypting: %v", err)
	}
	if opened != secret {
		t.Errorf("round trip changed the value: %q", opened)
	}
}

// A repeated nonce in GCM is catastrophic, so identical plaintexts must not
// produce identical ciphertexts.
func TestCipher_UsesAFreshNonce(t *testing.T) {
	c, _ := crypto.New(testKey)

	first, _ := c.Encrypt("same input")
	second, _ := c.Encrypt("same input")

	if string(first) == string(second) {
		t.Error("encrypting the same value twice produced identical output")
	}
}

func TestCipher_RejectsTamperedCiphertext(t *testing.T) {
	c, _ := crypto.New(testKey)
	sealed, _ := c.Encrypt("sensitive")

	sealed[len(sealed)-1] ^= 0xFF
	if _, err := c.Decrypt(sealed); err == nil {
		t.Error("a tampered ciphertext was accepted")
	}
}

func TestCipher_RejectsWrongKey(t *testing.T) {
	sealer, _ := crypto.New(testKey)
	sealed, _ := sealer.Encrypt("sensitive")

	other, _ := crypto.New("abcdefghabcdefghabcdefghabcdefgh")
	if _, err := other.Decrypt(sealed); err == nil {
		t.Error("a different key decrypted the payload")
	}
}

func TestCipher_RejectsBadKeyLength(t *testing.T) {
	if _, err := crypto.New("too-short"); err == nil {
		t.Error("expected a short key to be rejected")
	}
}

func TestCipher_HandlesEmptyValues(t *testing.T) {
	c, _ := crypto.New(testKey)

	sealed, err := c.Encrypt("")
	if err != nil || sealed != nil {
		t.Errorf("empty plaintext should seal to nil, got %v %v", sealed, err)
	}
	opened, err := c.Decrypt(nil)
	if err != nil || opened != "" {
		t.Errorf("nil ciphertext should open to empty, got %q %v", opened, err)
	}
}
