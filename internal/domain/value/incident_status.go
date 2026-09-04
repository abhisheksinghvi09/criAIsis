package value

import (
	"fmt"
	"strings"
)

// IncidentStatus models the finite lifecycle of an active war room.
// For MVP, incidents transition unidirectionally from investigating to resolved.
type IncidentStatus string

const (
	IncidentStatusInvestigating IncidentStatus = "investigating"
	IncidentStatusResolved      IncidentStatus = "resolved"
)

// ParseIncidentStatus validates status string representations from DB scans or external webhooks.
func ParseIncidentStatus(raw string) (IncidentStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "investigating":
		return IncidentStatusInvestigating, nil
	case "resolved":
		return IncidentStatusResolved, nil
	default:
		return "", fmt.Errorf("invalid incident status '%s': must be 'investigating' or 'resolved'", raw)
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
