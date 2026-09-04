package value

import (
	"fmt"

	"github.com/google/uuid"
)

// WorkspaceID strongly types the tenant identifier to prevent accidental
// parameter transposition at compile time across service boundaries.
type WorkspaceID struct {
	value uuid.UUID
}

// NewWorkspaceID generates a cryptographically secure random tenant ID.
func NewWorkspaceID() WorkspaceID {
	return WorkspaceID{value: uuid.New()}
}

// ParseWorkspaceID validates and constructs a WorkspaceID from an external UUID string.
func ParseWorkspaceID(raw string) (WorkspaceID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return WorkspaceID{}, fmt.Errorf("invalid workspace id '%s': %w", raw, err)
	}
	if parsed == uuid.Nil {
		return WorkspaceID{}, fmt.Errorf("nil workspace id is not permitted")
	}
	return WorkspaceID{value: parsed}, nil
}

// FromUUID wraps an existing validated UUID into a WorkspaceID.
func FromUUID(id uuid.UUID) (WorkspaceID, error) {
	if id == uuid.Nil {
		return WorkspaceID{}, fmt.Errorf("nil workspace id is not permitted")
	}
	return WorkspaceID{value: id}, nil
}

// UUID returns the underlying primitive UUID for database drivers and serialization.
func (w WorkspaceID) UUID() uuid.UUID {
	return w.value
}

// String implements fmt.Stringer for logging and tracing without exposing internal state.
func (w WorkspaceID) String() string {
	return w.value.String()
}

// IsZero checks whether the workspace identifier has been initialized.
func (w WorkspaceID) IsZero() bool {
	return w.value == uuid.Nil
}
