package value

import (
	"fmt"
	"strings"
)

// Severity classifies the business and technical impact of an active incident.
// Standardizing on 4 tiers (SEV-1 through SEV-4) aligns with industry SRE practices (PagerDuty, incident.io).
type Severity string

const (
	// SeveritySev1: Critical business outage. All hands on deck. Immediate customer impact.
	SeveritySev1 Severity = "sev-1"

	// SeveritySev2: Major service degradation. Partial outage or core dependency failure.
	SeveritySev2 Severity = "sev-2"

	// SeveritySev3: Minor disruption or intermittent errors. Non-blocking with workaround.
	SeveritySev3 Severity = "sev-3"

	// SeveritySev4: Cosmetic issue, internal tooling blip, or low-priority investigation.
	SeveritySev4 Severity = "sev-4"
)

// ParseSeverity normalizes incoming severity strings from Slack slash commands or dashboard inputs.
// It accepts formats like "SEV-1", "sev1", "sev 1", "critical", and maps them to canonical values.
func ParseSeverity(raw string) (Severity, error) {
	canonical := strings.ToLower(strings.TrimSpace(raw))
	canonical = strings.ReplaceAll(canonical, " ", "")
	canonical = strings.ReplaceAll(canonical, "_", "-")

	switch canonical {
	case "sev-1", "sev1", "critical":
		return SeveritySev1, nil
	case "sev-2", "sev2", "high", "major":
		return SeveritySev2, nil
	case "sev-3", "sev3", "medium", "minor":
		return SeveritySev3, nil
	case "sev-4", "sev4", "low":
		return SeveritySev4, nil
	default:
		return "", fmt.Errorf("invalid severity '%s': expected sev-1, sev-2, sev-3, or sev-4", raw)
	}
}

// String returns the canonical string representation for database storage and logging.
func (s Severity) String() string {
	return string(s)
}

// IsValid checks if the severity is one of the recognized tiers.
func (s Severity) IsValid() bool {
	switch s {
	case SeveritySev1, SeveritySev2, SeveritySev3, SeveritySev4:
		return true
	default:
		return false
	}
}
