// Package crypto encrypts the credentials criAIsis holds on a customer's behalf:
// Slack bot tokens and outbound notification webhook URLs.
//
// Both are bearer credentials for someone else's workspace, so they are encrypted
// at rest with AES-GCM-256 and never logged.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// KeySize is the exact key length AES-256 requires.
const KeySize = 32

// ErrKeyLength reports a key that is not exactly 32 bytes.
var ErrKeyLength = errors.New("encryption key must be exactly 32 bytes")

// Cipher seals and opens secrets with AES-GCM-256.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a cipher from the configured key.
func New(key string) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w, got %d", ErrKeyLength, len(key))
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("creating aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext, returning nonce||ciphertext. A fresh random nonce is
// generated per call, so encrypting the same token twice yields different bytes.
func (c *Cipher) Encrypt(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt opens a value produced by Encrypt. A tampered or truncated payload fails
// authentication rather than returning partial plaintext.
func (c *Cipher) Decrypt(sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}

	nonceSize := c.aead.NonceSize()
	if len(sealed) < nonceSize {
		return "", errors.New("ciphertext is shorter than the nonce")
	}

	plaintext, err := c.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}
	return string(plaintext), nil
}
