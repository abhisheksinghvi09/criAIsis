package entity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"criaisis/internal/domain/value"
)

// Workspace models a Slack tenant installation.
// It serves as the root isolation boundary for all subsequent data structures.
type Workspace struct {
	id                     value.WorkspaceID
	slackTeamID            string
	slackTeamName          string
	slackBotTokenEncrypted []byte
	webhookSecretHash      []byte
	adminAPIKeyHash        []byte
	createdAt              time.Time
	updatedAt              time.Time
}

// NewWorkspace creates a brand new tenant record from an OAuth installation callback.
func NewWorkspace(
	slackTeamID string,
	slackTeamName string,
	slackBotTokenEncrypted []byte,
) (*Workspace, error) {
	if strings.TrimSpace(slackTeamID) == "" {
		return nil, errors.New("slack team id cannot be empty")
	}
	if strings.TrimSpace(slackTeamName) == "" {
		return nil, errors.New("slack team name cannot be empty")
	}
	if len(slackBotTokenEncrypted) == 0 {
		return nil, errors.New("encrypted bot token cannot be empty")
	}

	now := time.Now().UTC()
	return &Workspace{
		id:                     value.NewWorkspaceID(),
		slackTeamID:            strings.TrimSpace(slackTeamID),
		slackTeamName:          strings.TrimSpace(slackTeamName),
		slackBotTokenEncrypted: slackBotTokenEncrypted,
		createdAt:              now,
		updatedAt:              now,
	}, nil
}

// ReconstituteWorkspace re-assembles an existing entity from persistent storage without regenerating timestamps or IDs.
func ReconstituteWorkspace(
	id value.WorkspaceID,
	slackTeamID string,
	slackTeamName string,
	slackBotTokenEncrypted []byte,
	webhookSecretHash []byte,
	adminAPIKeyHash []byte,
	createdAt time.Time,
	updatedAt time.Time,
) (*Workspace, error) {
	if id.IsZero() {
		return nil, errors.New("workspace id cannot be zero")
	}
	return &Workspace{
		id:                     id,
		slackTeamID:            slackTeamID,
		slackTeamName:          slackTeamName,
		slackBotTokenEncrypted: slackBotTokenEncrypted,
		webhookSecretHash:      webhookSecretHash,
		adminAPIKeyHash:        adminAPIKeyHash,
		createdAt:              createdAt,
		updatedAt:              updatedAt,
	}, nil
}

func (w *Workspace) ID() value.WorkspaceID          { return w.id }
func (w *Workspace) SlackTeamID() string            { return w.slackTeamID }
func (w *Workspace) SlackTeamName() string          { return w.slackTeamName }
func (w *Workspace) SlackBotTokenEncrypted() []byte { return w.slackBotTokenEncrypted }
func (w *Workspace) WebhookSecretHash() []byte      { return w.webhookSecretHash }
func (w *Workspace) AdminAPIKeyHash() []byte        { return w.adminAPIKeyHash }
func (w *Workspace) CreatedAt() time.Time           { return w.createdAt }
func (w *Workspace) UpdatedAt() time.Time           { return w.updatedAt }

// UpdateTeamName applies a name change (e.g. workspace rename in Slack).
func (w *Workspace) UpdateTeamName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("workspace team name cannot be empty")
	}
	w.slackTeamName = trimmed
	w.updatedAt = time.Now().UTC()
	return nil
}

// UpdateBotToken updates the encrypted bot token on re-installation.
func (w *Workspace) UpdateBotToken(tokenEncrypted []byte) error {
	if len(tokenEncrypted) == 0 {
		return errors.New("encrypted bot token cannot be empty")
	}
	w.slackBotTokenEncrypted = tokenEncrypted
	w.updatedAt = time.Now().UTC()
	return nil
}

// GenerateWebhookSecret issues a new alert-ingestion credential, storing only its
// digest. The returned plaintext is the caller's single opportunity to show it to
// an operator; it cannot be recovered afterwards.
func (w *Workspace) GenerateWebhookSecret() (string, error) {
	secret, digest, err := newSecret()
	if err != nil {
		return "", fmt.Errorf("generating webhook secret: %w", err)
	}
	w.webhookSecretHash = digest
	w.updatedAt = time.Now().UTC()
	return secret, nil
}

// VerifyWebhookSecret reports whether a presented token matches this workspace.
// The comparison is constant time, and a workspace with no configured secret
// rejects everything rather than accepting anything.
func (w *Workspace) VerifyWebhookSecret(presented string) bool {
	return verifySecret(w.webhookSecretHash, presented)
}

// GenerateAdminAPIKey issues the credential for this tenant's management API.
//
// It is deliberately separate from the alert ingestion token: a monitoring system
// holding the ingestion token can open incidents, but must not be able to read or
// rewrite the tenant's model credentials.
func (w *Workspace) GenerateAdminAPIKey() (string, error) {
	key, digest, err := newSecret()
	if err != nil {
		return "", fmt.Errorf("generating admin api key: %w", err)
	}
	w.adminAPIKeyHash = digest
	w.updatedAt = time.Now().UTC()
	return key, nil
}

// VerifyAdminAPIKey reports whether a presented key administers this workspace.
// A workspace with no key configured rejects everything.
func (w *Workspace) VerifyAdminAPIKey(presented string) bool {
	return verifySecret(w.adminAPIKeyHash, presented)
}

// newSecret mints a random credential and returns it alongside its digest.
func newSecret() (secret string, digest []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	secret = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	return secret, sum[:], nil
}

// verifySecret compares a presented credential against a stored digest in
// constant time, refusing everything when no credential is configured.
func verifySecret(digest []byte, presented string) bool {
	if len(digest) == 0 || presented == "" {
		return false
	}
	sum := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(sum[:], digest) == 1
}
