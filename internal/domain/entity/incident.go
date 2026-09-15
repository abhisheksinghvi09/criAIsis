package entity

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

var (
	ErrIncidentAlreadyResolved = errors.New("incident is already resolved")
)

// Incident serves as the aggregate root for a multi-agent debate session anchored to a Slack thread.
type Incident struct {
	id                 uuid.UUID
	workspaceID        value.WorkspaceID
	title              string
	description        string
	slackChannelID     value.SlackChannelID
	slackThreadTS      value.SlackThreadTS
	severity           value.Severity
	status             value.IncidentStatus
	createdBySlackUser value.SlackUserID
	metadata           json.RawMessage
	resolvedAt         *time.Time
	createdAt          time.Time
	updatedAt          time.Time
}

func NewIncident(
	workspaceID value.WorkspaceID,
	title string,
	description string,
	slackChannelID value.SlackChannelID,
	slackThreadTS value.SlackThreadTS,
	severity value.Severity,
	createdBySlackUser value.SlackUserID,
	metadata json.RawMessage,
) (*Incident, error) {
	if workspaceID.IsZero() {
		return nil, errors.New("workspace id cannot be zero")
	}
	trimmedTitle := strings.TrimSpace(title)
	if trimmedTitle == "" {
		return nil, errors.New("incident title cannot be empty")
	}
	trimmedDesc := strings.TrimSpace(description)
	if trimmedDesc == "" {
		return nil, errors.New("incident description cannot be empty")
	}
	if slackChannelID == "" {
		return nil, errors.New("slack channel id cannot be empty")
	}
	if slackThreadTS == "" {
		return nil, errors.New("slack thread ts cannot be empty")
	}
	if !severity.IsValid() {
		return nil, fmt.Errorf("invalid incident severity: '%s'", severity)
	}
	if createdBySlackUser == "" {
		return nil, errors.New("creator slack user id cannot be empty")
	}

	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	now := time.Now().UTC()
	return &Incident{
		id:                 uuid.New(),
		workspaceID:        workspaceID,
		title:              trimmedTitle,
		description:        trimmedDesc,
		slackChannelID:     slackChannelID,
		slackThreadTS:      slackThreadTS,
		severity:           severity,
		status:             value.IncidentStatusInvestigating,
		createdBySlackUser: createdBySlackUser,
		metadata:           metadata,
		resolvedAt:         nil,
		createdAt:          now,
		updatedAt:          now,
	}, nil
}

func ReconstituteIncident(
	id uuid.UUID,
	workspaceID value.WorkspaceID,
	title string,
	description string,
	slackChannelID value.SlackChannelID,
	slackThreadTS value.SlackThreadTS,
	severity value.Severity,
	status value.IncidentStatus,
	createdBySlackUser value.SlackUserID,
	metadata json.RawMessage,
	resolvedAt *time.Time,
	createdAt time.Time,
	updatedAt time.Time,
) *Incident {
	return &Incident{
		id:                 id,
		workspaceID:        workspaceID,
		title:              title,
		description:        description,
		slackChannelID:     slackChannelID,
		slackThreadTS:      slackThreadTS,
		severity:           severity,
		status:             status,
		createdBySlackUser: createdBySlackUser,
		metadata:           metadata,
		resolvedAt:         resolvedAt,
		createdAt:          createdAt,
		updatedAt:          updatedAt,
	}
}

func (i *Incident) ID() uuid.UUID                         { return i.id }
func (i *Incident) WorkspaceID() value.WorkspaceID        { return i.workspaceID }
func (i *Incident) Title() string                         { return i.title }
func (i *Incident) Description() string                   { return i.description }
func (i *Incident) SlackChannelID() value.SlackChannelID  { return i.slackChannelID }
func (i *Incident) SlackThreadTS() value.SlackThreadTS    { return i.slackThreadTS }
func (i *Incident) Severity() value.Severity              { return i.severity }
func (i *Incident) Status() value.IncidentStatus          { return i.status }
func (i *Incident) CreatedBySlackUser() value.SlackUserID { return i.createdBySlackUser }
func (i *Incident) Metadata() json.RawMessage             { return i.metadata }
func (i *Incident) ResolvedAt() *time.Time                { return i.resolvedAt }
func (i *Incident) CreatedAt() time.Time                  { return i.createdAt }
func (i *Incident) UpdatedAt() time.Time                  { return i.updatedAt }

// UpdateSeverity elevates or lowers incident severity during active investigation.
func (i *Incident) UpdateSeverity(severity value.Severity) error {
	if !severity.IsValid() {
		return fmt.Errorf("invalid severity: '%s'", severity)
	}
	i.severity = severity
	i.updatedAt = time.Now().UTC()
	return nil
}

// Resolve formally transitions the incident to resolved, locking the resolution timestamp.
// If already resolved, it returns ErrIncidentAlreadyResolved to protect lifecycle invariants.
func (i *Incident) Resolve(resolvedAt time.Time) error {
	if i.status.IsResolved() {
		return ErrIncidentAlreadyResolved
	}
	i.status = value.IncidentStatusResolved
	i.resolvedAt = &resolvedAt
	i.updatedAt = resolvedAt
	return nil
}

// TriggerType returns how the incident was initiated, defaulting to slash_command.
func (i *Incident) TriggerType() value.TriggerType {
	var meta struct {
		TriggerType string `json:"trigger_type"`
	}
	if len(i.metadata) > 0 && json.Unmarshal(i.metadata, &meta) == nil && meta.TriggerType != "" {
		if tt, err := value.ParseTriggerType(meta.TriggerType); err == nil {
			return tt
		}
	}
	return value.TriggerTypeSlashCommand
}

// SetTriggerType updates the trigger type in incident metadata.
func (i *Incident) SetTriggerType(tt value.TriggerType) error {
	if !tt.IsValid() {
		return fmt.Errorf("invalid trigger type: '%s'", tt)
	}
	meta := make(map[string]any)
	if len(i.metadata) > 0 {
		_ = json.Unmarshal(i.metadata, &meta)
	}
	meta["trigger_type"] = tt.String()
	bytes, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	i.metadata = bytes
	i.updatedAt = time.Now().UTC()
	return nil
}

// IncidentContext extracts structured telemetry evidence from metadata, if present.
func (i *Incident) IncidentContext() (*value.IncidentContext, error) {
	var meta struct {
		Context *value.IncidentContext `json:"incident_context"`
	}
	if len(i.metadata) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(i.metadata, &meta); err != nil {
		return nil, err
	}
	return meta.Context, nil
}

// SetIncidentContext stores structured telemetry evidence in incident metadata.
func (i *Incident) SetIncidentContext(ctx value.IncidentContext) error {
	meta := make(map[string]any)
	if len(i.metadata) > 0 {
		_ = json.Unmarshal(i.metadata, &meta)
	}
	meta["incident_context"] = ctx
	bytes, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	i.metadata = bytes
	i.updatedAt = time.Now().UTC()
	return nil
}
