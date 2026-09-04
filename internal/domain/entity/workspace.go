package entity

import (
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
		createdAt:              createdAt,
		updatedAt:              updatedAt,
	}, nil
}

func (w *Workspace) ID() value.WorkspaceID              { return w.id }
func (w *Workspace) SlackTeamID() string               { return w.slackTeamID }
func (w *Workspace) SlackTeamName() string             { return w.slackTeamName }
func (w *Workspace) SlackBotTokenEncrypted() []byte    { return w.slackBotTokenEncrypted }
func (w *Workspace) CreatedAt() time.Time              { return w.createdAt }
func (w *Workspace) UpdatedAt() time.Time              { return w.updatedAt }

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
