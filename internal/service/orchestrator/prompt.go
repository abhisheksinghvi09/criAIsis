package orchestrator

import (
	"fmt"
	"sort"
	"strings"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
)

// mandate is prepended to every prompt. It is the product's central boundary, so it
// is stated once here rather than trusted to each persona's editable system prompt.
const mandate = `You are a read-only incident investigator. You NEVER mutate production:
no restarts, no config changes, no writes, no destructive commands. You may only
read, reason, and hand a human exact commands to run themselves.
If your runbooks do not cover this incident, say so plainly and set confidence to
"none". Inventing plausible-sounding guidance is worse than admitting a gap.`

// specialistPrompt builds the Stage 1 user message for one persona.
func specialistPrompt(inc *entity.Incident, results []*repository.SearchResult, probes []string) string {
	var sb strings.Builder

	sb.WriteString("# Active incident\n")
	sb.WriteString(incidentBrief(inc))

	sb.WriteString("\n# Your runbook excerpts\n")
	if len(results) == 0 {
		sb.WriteString("(none matched this incident in your domain's runbooks)\n")
	}
	for _, res := range results {
		fmt.Fprintf(&sb, "\n[chunk_id: %s] from %q\n%s\n", res.Chunk.ID(), res.DocumentTitle, res.Chunk.ChunkText())
	}

	if len(probes) > 0 {
		sb.WriteString("\n# Live telemetry probes\n")
		for _, probe := range probes {
			fmt.Fprintf(&sb, "- %s\n", probe)
		}
	}

	sb.WriteString("\n# Your task\n")
	sb.WriteString("Assess this incident strictly from your own domain. Cite only the chunk IDs listed above.\n")
	sb.WriteString("If the evidence points away from your domain, say so and set owns_this to false.\n")
	return sb.String()
}

// synthesisPrompt builds the Stage 2 adversarial cross-examination message.
func synthesisPrompt(inc *entity.Incident, outcomes []SpecialistOutcome) string {
	var sb strings.Builder

	sb.WriteString("# Active incident\n")
	sb.WriteString(incidentBrief(inc))

	sb.WriteString("\n# Stage 1 specialist hypotheses\n")
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			fmt.Fprintf(&sb, "\n## %s\n(no hypothesis: %v)\n", outcome.Persona.DisplayName(), outcome.Err)
			continue
		}
		fmt.Fprintf(&sb, "\n## %s (confidence: %s, claims ownership: %t)\n%s\n",
			outcome.Persona.DisplayName(), outcome.Result.Confidence, outcome.Result.OwnsThis, outcome.Result.Hypothesis)

		writeList(&sb, "Evidence", outcome.Result.Evidence)
		writeList(&sb, "Proposed diagnostics", outcome.Result.DiagnosticQueries)
		if len(outcome.Result.CitedChunkIDs) > 0 {
			sb.WriteString("Cited chunks: ")
			for _, id := range outcome.Result.CitedChunkIDs {
				fmt.Fprintf(&sb, "[chunk_id: %s] ", id)
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n# Your task\n")
	sb.WriteString(`Cross-examine the specialists above as the Incident Commander.
Name the direct contradictions and resolve them on the evidence, not on confidence
levels. A specialist claiming ownership loudly is not evidence.

Then classify the root cause:
- "code": a release introduced a defect (bad query, unhandled panic, breaking payload).
- "infra": capacity, hardware, AZ impairment, packet loss, node pressure.
- "hybrid": a code change that triggered an infrastructure collapse.

Cite only chunk IDs that a specialist above actually cited.
`)
	return sb.String()
}

// followUpPrompt builds a Stage 3 targeted reply to an engineer's in-thread question.
func followUpPrompt(inc *entity.Incident, question string, transcript []*entity.DebateTurn, results []*repository.SearchResult) string {
	var sb strings.Builder

	sb.WriteString("# Active incident\n")
	sb.WriteString(incidentBrief(inc))

	if len(transcript) > 0 {
		sb.WriteString("\n# Debate so far\n")
		for _, turn := range transcript {
			fmt.Fprintf(&sb, "\n[stage %d / %s]\n%s\n", turn.Stage().Int(), turn.TurnType(), turn.Content())
		}
	}

	sb.WriteString("\n# Your runbook excerpts\n")
	if len(results) == 0 {
		sb.WriteString("(none matched this question in your domain's runbooks)\n")
	}
	for _, res := range results {
		fmt.Fprintf(&sb, "\n[chunk_id: %s] from %q\n%s\n", res.Chunk.ID(), res.DocumentTitle, res.Chunk.ChunkText())
	}

	fmt.Fprintf(&sb, "\n# The engineer asks\n%s\n", question)
	sb.WriteString("\nAnswer from your runbooks. Give exact read-only commands they can paste.\n")
	return sb.String()
}

// personaSystemPrompt binds the operator-editable persona prompt to the read-only mandate.
func personaSystemPrompt(persona *entity.Persona) string {
	return mandate + "\n\n" + persona.SystemPrompt()
}

// synthesisSystemPrompt frames the Stage 2 agent as the Incident Commander.
func synthesisSystemPrompt() string {
	return mandate + `

You are the Incident Commander. Four domain specialists have each filed a hypothesis
grounded in their own runbooks. They cannot see each other's work, so they will
overlap, and some will claim an incident that is not theirs. Your job is to find the
single most probable root cause and say why the others lose.`
}

// incidentBrief renders the incident and its ingested telemetry evidence.
func incidentBrief(inc *entity.Incident) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Title: %s\nSeverity: %s\nTrigger: %s\n\n%s\n", inc.Title(), inc.Severity(), inc.TriggerType(), inc.Description())

	ctx, err := inc.IncidentContext()
	if err != nil || ctx == nil || ctx.IsEmpty() {
		return sb.String()
	}

	if name := ctx.AlertName(); name != "" {
		fmt.Fprintf(&sb, "\nAlert: %s (via %s)\n", name, ctx.Provider())
	}
	writeList(&sb, "Error logs", ctx.ErrorLogs())
	writeList(&sb, "Stack traces", ctx.StackTraces())

	if metrics := ctx.Metrics(); len(metrics) > 0 {
		sb.WriteString("\nMetrics:\n")
		for _, key := range sortedKeys(metrics) {
			fmt.Fprintf(&sb, "- %s = %g\n", key, metrics[key])
		}
	}
	return sb.String()
}

// writeList renders a titled bullet list, skipping empty sections.
func writeList(sb *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(sb, "\n%s:\n", title)
	for _, item := range items {
		fmt.Fprintf(sb, "- %s\n", item)
	}
}

// sortedKeys keeps metric ordering stable so prompts stay cacheable and diffable.
func sortedKeys(metrics map[string]float64) []string {
	keys := make([]string, 0, len(metrics))
	for key := range metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
