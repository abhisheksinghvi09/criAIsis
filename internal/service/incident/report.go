package incident

import (
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/infrastructure/notify"
	"criaisis/internal/service/orchestrator"
)

// maxCitations caps how many runbook sources are listed. Past a handful the list
// stops being evidence and starts being noise in a chat message.
const maxCitations = 6

// buildReport turns a finished clash into the message an on-call engineer reads.
func buildReport(inc *entity.Incident, result *orchestrator.ClashResult, elapsed time.Duration) notify.Report {
	return notify.Report{
		IncidentTitle:  inc.Title(),
		Severity:       inc.Severity().String(),
		Trigger:        inc.TriggerType().String(),
		Consensus:      result.Synthesis.Consensus,
		Classification: result.Synthesis.Classification,
		OwningDomain:   result.Synthesis.OwningDomain,
		Contradictions: result.Synthesis.Contradictions,
		NextSteps:      result.Synthesis.NextSteps,
		Queries:        result.Synthesis.DiagnosticQueries,
		Specialists:    summarizeSpecialists(result),
		Citations:      collectCitations(result),
		Elapsed:        elapsed.Round(100 * time.Millisecond).String(),
	}
}

// summarizeSpecialists records every domain's position, including the ones that
// failed: a silent absence would read as agreement.
func summarizeSpecialists(result *orchestrator.ClashResult) []notify.SpecialistSummary {
	summaries := make([]notify.SpecialistSummary, 0, len(result.Stage1))
	for _, outcome := range result.Stage1 {
		if outcome.Err != nil {
			summaries = append(summaries, notify.SpecialistSummary{
				Persona:    outcome.Persona.DisplayName(),
				Failed:     true,
				FailReason: outcome.Err.Error(),
			})
			continue
		}
		summaries = append(summaries, notify.SpecialistSummary{
			Persona:    outcome.Persona.DisplayName(),
			Confidence: outcome.Result.Confidence,
			OwnsThis:   outcome.Result.OwnsThis,
			Hypothesis: outcome.Result.Hypothesis,
		})
	}
	return summaries
}

// collectCitations lists the distinct runbooks the debate actually rested on.
func collectCitations(result *orchestrator.ClashResult) []string {
	seen := map[string]bool{}
	var titles []string

	turns := []*entity.DebateTurn{result.SynthesisTurn}
	for _, outcome := range result.Stage1 {
		if outcome.Turn != nil {
			turns = append(turns, outcome.Turn)
		}
	}

	for _, turn := range turns {
		citations, err := turn.Citations()
		if err != nil {
			continue
		}
		for _, citation := range citations {
			if seen[citation.DocumentTitle] || len(titles) >= maxCitations {
				continue
			}
			seen[citation.DocumentTitle] = true
			titles = append(titles, citation.DocumentTitle)
		}
	}

	if len(titles) == 0 {
		return []string{"none: no runbook matched this incident"}
	}
	return titles
}
