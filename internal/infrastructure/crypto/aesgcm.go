// Package crypto encrypts the credentials criAIsis holds on a customer's behalf:
// Slack bot tokens, model and embedding keys, webhook URLs and on-call keys.
//
// They are bearer credentials for someone else's systems, so each is sealed with
// AES-256-GCM in a versioned envelope:
//
//	"cr2" | key id (1 byte) | nonce (12 bytes) | ciphertext+tag
//
// The key id makes rotation possible: a keyring holds the current key, used for
// every new seal, plus previous keys that can still open older values until
// they are rewrapped. Each value is also bound to where it lives (associated
// data such as the tenant and field), so a ciphertext copied into another
// tenant's row or another column fails authentication instead of decrypting.
//
// Values written before the envelope existed (nonce|ciphertext, no binding)
// still open, and NeedsRewrap reports them so they can be upgraded in place.
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// KeySize is the exact key length AES-256 requires.
const KeySize = 32

// ErrKeyLength reports a key that is not 32 bytes.
var ErrKeyLength = errors.New("encryption key must be 32 bytes (32 characters, or base64: followed by 32 encoded bytes)")

// errOpen is deliberately uninformative: which key or binding failed is not
// something to reveal.
var errOpen = errors.New("decrypting: authentication failed")

var envelopeMagic = []byte("cr2")

const nonceSize = 12

// Cipher seals and opens secrets with a keyring.
type Cipher struct {
	current byte
	keys    map[byte]cipher.AEAD
	ids     []byte // every key id, current first: legacy values try them in order
}

// ParseKey accepts either exactly 32 raw characters or "base64:" followed by the
// standard base64 encoding of 32 random bytes. Prefer the second: 32 typed
// characters carry far less than 256 bits of entropy.
func ParseKey(raw string) ([]byte, error) {
	if b64, ok := strings.CutPrefix(raw, "base64:"); ok {
		key, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(key) != KeySize {
			return nil, ErrKeyLength
		}
		return key, nil
	}
	if len(raw) != KeySize {
		return nil, fmt.Errorf("%w, got %d", ErrKeyLength, len(raw))
	}
	return []byte(raw), nil
}

// New builds a single-key cipher (key id 1).
func New(key string) (*Cipher, error) { return NewKeyring(1, key, nil) }

// NewKeyring builds a cipher that seals with currentKey under currentID and can
// still open values sealed under any previous key.
func NewKeyring(currentID int, currentKey string, previous map[int]string) (*Cipher, error) {
	c := &Cipher{keys: map[byte]cipher.AEAD{}}
	add := func(id int, raw string) (byte, error) {
		if id < 1 || id > 255 {
			return 0, fmt.Errorf("encryption key id %d is out of range 1-255", id)
		}
		kid := byte(id)
		if _, dup := c.keys[kid]; dup {
			return 0, fmt.Errorf("encryption key id %d is configured twice", id)
		}
		key, err := ParseKey(raw)
		if err != nil {
			return 0, fmt.Errorf("key %d: %w", id, err)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return 0, fmt.Errorf("creating aes cipher: %w", err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return 0, fmt.Errorf("creating gcm: %w", err)
		}
		c.keys[kid] = aead
		c.ids = append(c.ids, kid)
		return kid, nil
	}

	current, err := add(currentID, currentKey)
	if err != nil {
		return nil, err
	}
	c.current = current
	for id, raw := range previous {
		if _, err := add(id, raw); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Seal encrypts plaintext under the current key, bound to aad. Empty plaintext
// stays empty, so "not configured" is never confused with a sealed value.
func (c *Cipher) Seal(plaintext, aad string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	out := make([]byte, 0, len(envelopeMagic)+1+nonceSize+len(plaintext)+16)
	out = append(out, envelopeMagic...)
	out = append(out, c.current)
	out = append(out, nonce...)
	return c.keys[c.current].Seal(out, nonce, []byte(plaintext), []byte(aad)), nil
}

// Open decrypts a value from Seal (or a legacy value) that was bound to aad.
// A tampered value, the wrong key, or the wrong binding all fail the same way.
func (c *Cipher) Open(sealed []byte, aad string) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	if id, nonce, body, ok := c.parseEnvelope(sealed); ok {
		if aead, known := c.keys[id]; known {
			if pt, err := aead.Open(nil, nonce, body, []byte(aad)); err == nil {
				return string(pt), nil
			}
		}
		// Fall through: a legacy value can begin with the magic bytes by chance.
	}
	if pt, ok := c.openLegacy(sealed); ok {
		return pt, nil
	}
	return "", errOpen
}

// NeedsRewrap reports whether a stored value should be re-sealed: it predates the
// envelope, or it was sealed under a key that is no longer current.
func (c *Cipher) NeedsRewrap(sealed []byte) bool {
	if len(sealed) == 0 {
		return false
	}
	id, _, _, ok := c.parseEnvelope(sealed)
	return !ok || id != c.current
}

func (c *Cipher) parseEnvelope(sealed []byte) (id byte, nonce, body []byte, ok bool) {
	head := len(envelopeMagic) + 1 + nonceSize
	if len(sealed) < head+16 || !bytes.HasPrefix(sealed, envelopeMagic) {
		return 0, nil, nil, false
	}
	id = sealed[len(envelopeMagic)]
	return id, sealed[len(envelopeMagic)+1 : head], sealed[head:], true
}

// openLegacy opens a pre-envelope value (nonce|ciphertext, no binding) with
// whichever configured key sealed it.
func (c *Cipher) openLegacy(sealed []byte) (string, bool) {
	if len(sealed) < nonceSize+16 {
		return "", false
	}
	for _, id := range c.ids {
		if pt, err := c.keys[id].Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil); err == nil {
			return string(pt), true
		}
	}
	return "", false
}

// The associated data for each stored secret. Changing a string here makes every
// value sealed under it unreadable, so these are part of the storage format.
const (
	FieldLLMKey        = "llm_api_key"
	FieldEmbeddingKey  = "embedding_api_key"
	FieldNotifyWebhook = "notify_webhook_url"
	FieldOnCallKey     = "oncall_api_key"
)

// BotTokenAAD binds a Slack bot token to the Slack team it was issued for. The
// team id is known before the workspace row exists, which the install needs.
func BotTokenAAD(slackTeamID string) string { return "slack_bot_token|" + slackTeamID }

// SettingAAD binds a tenant credential to its workspace and field.
func SettingAAD(workspaceID, field string) string {
	return "workspace_setting|" + workspaceID + "|" + field
}
