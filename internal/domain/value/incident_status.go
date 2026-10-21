package value

import (
	"fmt"
	"strings"
)

// IncidentStatus models the finite lifecycle of an active war room.
// Incidents move from investigating to resolved, or to failed when the clash
// could not complete (an operator may still resolve a failed one).
type IncidentStatus string

const (
	IncidentStatusInvestigating IncidentStatus = "investigating"
	IncidentStatusResolved      IncidentStatus = "resolved"
	IncidentStatusFailed        IncidentStatus = "failed"
)

// ParseIncidentStatus validates status string representations from DB scans or external webhooks.
func ParseIncidentStatus(raw string) (IncidentStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "investigating":
		return IncidentStatusInvestigating, nil
	case "resolved":
		return IncidentStatusResolved, nil
	case "failed":
		return IncidentStatusFailed, nil
	default:
		return "", fmt.Errorf("invalid incident status '%s': must be 'investigating', 'resolved' or 'failed'", raw)
	}
}

// String returns the raw string representation.
func (s IncidentStatus) String() string {
	return string(s)
}

// IsResolved returns true if the incident has formally concluded.
func (s IncidentStatus) IsResolved() bool {
	return s == IncidentStatusResolved
}
