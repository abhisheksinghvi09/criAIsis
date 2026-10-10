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

// plainText renders the whole report as text. Discord uses it directly and Slack
// uses it as the notification fallback.
func (r Report) plainText() string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "%s\n%s\n\n", r.IncidentTitle, r.headline())
	fmt.Fprintf(&sb, "%s\n", r.Consensus)

	writeSection(&sb, "Contradictions resolved", r.Contradictions)
	writeSection(&sb, "Next steps", r.NextSteps)
	writeSection(&sb, "Run these (read-only)", r.Queries)

	if len(r.Specialists) > 0 {
		sb.WriteString("\nSpecialist positions\n")
		for _, s := range r.Specialists {
			if s.Failed {
				fmt.Fprintf(&sb, "- %s: no hypothesis (%s)\n", s.Persona, s.FailReason)
				continue
			}
			fmt.Fprintf(&sb, "- %s (%s confidence%s): %s\n",
				s.Persona, s.Confidence, ownershipNote(s.OwnsThis), s.Hypothesis)
		}
	}

	writeSection(&sb, "Cited runbooks", r.Citations)
	fmt.Fprintf(&sb, "\nInvestigated in %s. criAIsis is read-only and has changed nothing.\n", r.Elapsed)

	return sb.String()
}

// ownershipNote marks the specialists who claimed the incident.
func ownershipNote(owns bool) string {
	if owns {
		return ", claims ownership"
	}
	return ""
}

// writeSection renders a titled list, skipping empty ones.
func writeSection(sb *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(sb, "\n%s\n", title)
	for _, item := range items {
		fmt.Fprintf(sb, "- %s\n", item)
	}
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
