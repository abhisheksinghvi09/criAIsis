package notify

import (
	"fmt"
	"strings"
)

// Report is what an on-call engineer actually reads: the verdict first, then the
// evidence behind it. It is provider-neutral; each notifier renders it natively.
type Report struct {
	IncidentTitle  string
	Severity       string
	Trigger        string
	Consensus      string
	Classification string
	OwningDomain   string
	Contradictions []string
	NextSteps      []string
	Queries        []string
	Specialists    []SpecialistSummary
	Citations      []string
	Elapsed        string
}

// SpecialistSummary is one domain's position in the debate.
type SpecialistSummary struct {
	Persona    string
	Confidence string
	OwnsThis   bool
	Hypothesis string
	Failed     bool
	FailReason string
}

// classificationLabel renders the root-cause verdict in the words the product uses,
// rather than the raw enum an engineer would have to decode.
func classificationLabel(classification string) string {
	switch strings.ToLower(strings.TrimSpace(classification)) {
	case "code":
		return "Code-level"
	case "infra":
		return "Infrastructure"
	case "hybrid":
		return "Hybrid (code triggered an infrastructure failure)"
	default:
		return classification
	}
}

// headline is the single line that must survive truncation in a notification
// preview, so it leads with what the commander concluded.
func (r Report) headline() string {
	return fmt.Sprintf("%s · %s · owned by %s",
		strings.ToUpper(r.Severity), classificationLabel(r.Classification), r.OwningDomain)
}

// ownershipNote marks the specialists who claimed the incident.
func ownershipNote(owns bool) string {
	if owns {
		return ", claims ownership"
	}
	return ""
}

// truncate keeps a field inside a provider's payload limit without cutting a
// multi-byte character in half.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	runes := []rune(text)
	for len(string(runes)) > limit-3 && len(runes) > 0 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}
