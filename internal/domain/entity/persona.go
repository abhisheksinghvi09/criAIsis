package entity

import (
	"errors"
	"fmt"
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

// defaultPersonaSpecs is the fixed roster of four domain specialists. Keeping the
// prompts here rather than in a migration lets an operator edit them per workspace
// without a schema change, while every new workspace still starts from the same baseline.
var defaultPersonaSpecs = []struct {
	key         value.PersonaKey
	displayName string
	prompt      string
}{
	{
		value.PersonaKeyNetwork, "Network Specialist",
		`You own the network path: routing, DNS, load balancers, CDN, service mesh,
ingress and egress, TLS termination, packet loss and cross-AZ latency.
Connection errors are frequently blamed on the network when the fault is at the
far end of a healthy socket. Establish whether packets are actually failing to
arrive before claiming this incident.`,
	},
	{
		value.PersonaKeyDatabase, "Database Specialist",
		`You own the data tier: connection pools, lock and deadlock contention, slow
and unindexed queries, replication lag, vacuum and bloat, disk and IOPS limits.
Distinguish a database that is genuinely saturated from one that is correctly
refusing work created by a caller. Exhausted connections are usually a symptom
of caller behaviour, not of database capacity.`,
	},
	{
		value.PersonaKeyApplication, "Application Specialist",
		`You own the service code: recent deploys, unhandled panics, memory leaks and
OOM kills, goroutine and thread leaks, transaction scoping, retry storms, and
breaking payload changes to downstream services.
When an incident begins shortly after a release, say so and name the suspect change.`,
	},
	{
		value.PersonaKeySecurity, "Security Specialist",
		`You own the adversarial view: DDoS and traffic floods, credential stuffing,
certificate expiry, IAM and permission changes, secret rotation, and anomalous
access patterns.
Most incidents are not attacks. Rule your domain in or out quickly and explicitly,
rather than speculating, so the commander can discount it.`,
	},
}

// DefaultPersonas builds the four specialists a newly installed workspace starts with.
func DefaultPersonas(workspaceID value.WorkspaceID) ([]*Persona, error) {
	personas := make([]*Persona, 0, len(defaultPersonaSpecs))
	for _, spec := range defaultPersonaSpecs {
		persona, err := NewPersona(workspaceID, spec.key, spec.displayName, spec.prompt)
		if err != nil {
			return nil, fmt.Errorf("building default %s persona: %w", spec.key, err)
		}
		personas = append(personas, persona)
	}
	return personas, nil
}
