package entity

import (
	"errors"
	"strings"
	"time"

	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// Persona encapsulates the role definition, system prompt, and availability toggle for an AI specialist.
type Persona struct {
	id           uuid.UUID
	workspaceID  value.WorkspaceID
	key          value.PersonaKey
	displayName  string
	systemPrompt string
	isEnabled    bool
	createdAt    time.Time
	updatedAt    time.Time
}

func NewPersona(
	workspaceID value.WorkspaceID,
	key value.PersonaKey,
	displayName string,
	systemPrompt string,
) (*Persona, error) {
	if workspaceID.IsZero() {
		return nil, errors.New("workspace id cannot be zero")
	}
	trimmedName := strings.TrimSpace(displayName)
	if trimmedName == "" {
		return nil, errors.New("display name cannot be empty")
	}
	trimmedPrompt := strings.TrimSpace(systemPrompt)
	if trimmedPrompt == "" {
		return nil, errors.New("system prompt cannot be empty")
	}

	now := time.Now().UTC()
	return &Persona{
		id:           uuid.New(),
		workspaceID:  workspaceID,
		key:          key,
		displayName:  trimmedName,
		systemPrompt: trimmedPrompt,
		isEnabled:    true,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

func ReconstitutePersona(
	id uuid.UUID,
	workspaceID value.WorkspaceID,
	key value.PersonaKey,
	displayName string,
	systemPrompt string,
	isEnabled bool,
	createdAt time.Time,
	updatedAt time.Time,
) *Persona {
	return &Persona{
		id:           id,
		workspaceID:  workspaceID,
		key:          key,
		displayName:  displayName,
		systemPrompt: systemPrompt,
		isEnabled:    isEnabled,
		createdAt:    createdAt,
		updatedAt:    updatedAt,
	}
}

func (p *Persona) ID() uuid.UUID                  { return p.id }
func (p *Persona) WorkspaceID() value.WorkspaceID { return p.workspaceID }
func (p *Persona) Key() value.PersonaKey          { return p.key }
func (p *Persona) DisplayName() string            { return p.displayName }
func (p *Persona) SystemPrompt() string           { return p.systemPrompt }
func (p *Persona) IsEnabled() bool                { return p.isEnabled }
func (p *Persona) CreatedAt() time.Time           { return p.createdAt }
func (p *Persona) UpdatedAt() time.Time           { return p.updatedAt }

// UpdatePrompt modifies the specialist grounding instructions while bumping the update timestamp.
func (p *Persona) UpdatePrompt(prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return errors.New("system prompt cannot be empty")
	}
	p.systemPrompt = trimmed
	p.updatedAt = time.Now().UTC()
	return nil
}

// Enable activates the persona for Stage 1 clash participation.
func (p *Persona) Enable() {
	p.isEnabled = true
	p.updatedAt = time.Now().UTC()
}

// Disable excludes the persona from future debates.
func (p *Persona) Disable() {
	p.isEnabled = false
	p.updatedAt = time.Now().UTC()
}
