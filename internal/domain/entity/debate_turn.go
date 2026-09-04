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

// DebateTurn records an immutable message turn produced during an incident clash.
// Citations are preserved in two complementary ways:
// 1. referencedChunkIDs []uuid.UUID allows relational joins and DB referential checks.
// 2. metadata JSONB stores immutable CitationSnapshot slices (title, excerpt, snippet) to ensure post-mortems survive document modification.
type DebateTurn struct {
	id                 uuid.UUID
	incidentID         uuid.UUID
	workspaceID        value.WorkspaceID
	personaID          *uuid.UUID
	stage              value.Stage
	turnType           value.TurnType
	content            string
	referencedChunkIDs []uuid.UUID
	slackMessageTS     string
	metadata           json.RawMessage
	createdAt          time.Time
}

func NewDebateTurn(
	incidentID uuid.UUID,
	workspaceID value.WorkspaceID,
	personaID *uuid.UUID,
	stage value.Stage,
	turnType value.TurnType,
	content string,
	referencedChunkIDs []uuid.UUID,
	slackMessageTS string,
	metadata json.RawMessage,
) (*DebateTurn, error) {
	if incidentID == uuid.Nil {
		return nil, errors.New("incident id cannot be nil")
	}
	if workspaceID.IsZero() {
		return nil, errors.New("workspace id cannot be zero")
	}
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("turn content cannot be empty")
	}

	if referencedChunkIDs == nil {
		referencedChunkIDs = make([]uuid.UUID, 0)
	}
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	return &DebateTurn{
		id:                 uuid.New(),
		incidentID:         incidentID,
		workspaceID:        workspaceID,
		personaID:          personaID,
		stage:              stage,
		turnType:           turnType,
		content:            content,
		referencedChunkIDs: referencedChunkIDs,
		slackMessageTS:     slackMessageTS,
		metadata:           metadata,
		createdAt:          time.Now().UTC(),
	}, nil
}

func ReconstituteDebateTurn(
	id uuid.UUID,
	incidentID uuid.UUID,
	workspaceID value.WorkspaceID,
	personaID *uuid.UUID,
	stage value.Stage,
	turnType value.TurnType,
	content string,
	referencedChunkIDs []uuid.UUID,
	slackMessageTS string,
	metadata json.RawMessage,
	createdAt time.Time,
) *DebateTurn {
	return &DebateTurn{
		id:                 id,
		incidentID:         incidentID,
		workspaceID:        workspaceID,
		personaID:          personaID,
		stage:              stage,
		turnType:           turnType,
		content:            content,
		referencedChunkIDs: referencedChunkIDs,
		slackMessageTS:     slackMessageTS,
		metadata:           metadata,
		createdAt:          createdAt,
	}
}

func (t *DebateTurn) ID() uuid.UUID                   { return t.id }
func (t *DebateTurn) IncidentID() uuid.UUID           { return t.incidentID }
func (t *DebateTurn) WorkspaceID() value.WorkspaceID  { return t.workspaceID }
func (t *DebateTurn) PersonaID() *uuid.UUID           { return t.personaID }
func (t *DebateTurn) Stage() value.Stage              { return t.stage }
func (t *DebateTurn) TurnType() value.TurnType        { return t.turnType }
func (t *DebateTurn) Content() string                 { return t.content }
func (t *DebateTurn) ReferencedChunkIDs() []uuid.UUID { return t.referencedChunkIDs }
func (t *DebateTurn) SlackMessageTS() string          { return t.slackMessageTS }
func (t *DebateTurn) Metadata() json.RawMessage       { return t.metadata }
func (t *DebateTurn) CreatedAt() time.Time            { return t.createdAt }

// Citations parses immutable citation snapshots stored inside the metadata JSONB extension slot.
func (t *DebateTurn) Citations() ([]value.CitationSnapshot, error) {
	if len(t.metadata) == 0 {
		return nil, nil
	}

	var envelope struct {
		Citations []value.CitationSnapshot `json:"citations"`
	}
	if err := json.Unmarshal(t.metadata, &envelope); err != nil {
		return nil, fmt.Errorf("failed to unmarshal citations from turn metadata: %w", err)
	}

	return envelope.Citations, nil
}
