// Package orchestrator implements the 2-Stage Asynchronous Clash: concurrent
// specialist hypotheses (Stage 1), adversarial consensus synthesis (Stage 2), and
// targeted in-thread follow-ups (Stage 3).
package orchestrator

import (
	"encoding/json"

	"criaisis/internal/domain/value"
)

// SpecialistResult is the structured Stage 1 output of one domain persona.
// Structured output is used rather than prose so citations and ownership survive
// into the transcript without brittle text parsing.
type SpecialistResult struct {
	Hypothesis        string   `json:"hypothesis"`
	Confidence        string   `json:"confidence"`
	OwnsThis          bool     `json:"owns_this"`
	Evidence          []string `json:"evidence"`
	CitedChunkIDs     []string `json:"cited_chunk_ids"`
	DiagnosticQueries []string `json:"diagnostic_queries"`
}

// SynthesisResult is the structured Stage 2 verdict from the Incident Commander agent.
type SynthesisResult struct {
	Consensus         string   `json:"consensus"`
	Classification    string   `json:"classification"`
	OwningDomain      string   `json:"owning_domain"`
	Contradictions    []string `json:"contradictions"`
	NextSteps         []string `json:"next_steps"`
	DiagnosticQueries []string `json:"diagnostic_queries"`
	CitedChunkIDs     []string `json:"cited_chunk_ids"`
}

// ParsedClassification converts the synthesis verdict into the domain value object.
func (s SynthesisResult) ParsedClassification() (value.IssueClassification, error) {
	return value.ParseIssueClassification(s.Classification)
}

// turnMetadata is the JSONB payload persisted alongside every debate turn. It carries
// the immutable citation snapshots that keep post-mortems readable after a runbook
// is edited or deleted.
type turnMetadata struct {
	Citations         []value.CitationSnapshot `json:"citations"`
	Confidence        string                   `json:"confidence,omitempty"`
	Classification    string                   `json:"classification,omitempty"`
	OwningDomain      string                   `json:"owning_domain,omitempty"`
	Contradictions    []string                 `json:"contradictions,omitempty"`
	NextSteps         []string                 `json:"next_steps,omitempty"`
	DiagnosticQueries []string                 `json:"diagnostic_queries,omitempty"`
}

// JSON renders the metadata envelope, falling back to an empty object so a
// marshalling slip can never block an append-only audit write.
func (m turnMetadata) JSON() json.RawMessage {
	raw, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// specialistSchema constrains Stage 1 responses.
func specialistSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"hypothesis": map[string]any{
				"type":        "string",
				"description": "Your domain assessment of this incident, in two to four sentences.",
			},
			"confidence": map[string]any{
				"type": "string",
				"enum": []string{"high", "medium", "low", "none"},
			},
			"owns_this": map[string]any{
				"type":        "boolean",
				"description": "True only if the root cause plausibly sits in your domain.",
			},
			"evidence": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Specific log lines, stack frames, or metrics supporting the hypothesis.",
			},
			"cited_chunk_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "IDs of the runbook excerpts you relied on. Use only IDs shown to you.",
			},
			"diagnostic_queries": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Read-only commands a human can paste to confirm or refute this.",
			},
		},
		"required":             []string{"hypothesis", "confidence", "owns_this", "evidence", "cited_chunk_ids", "diagnostic_queries"},
		"additionalProperties": false,
	}
}

// synthesisSchema constrains the Stage 2 consensus verdict.
func synthesisSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"consensus": map[string]any{
				"type":        "string",
				"description": "The single most probable root cause, and why the competing hypotheses lose.",
			},
			"classification": map[string]any{
				"type": "string",
				"enum": []string{"code", "infra", "hybrid"},
			},
			"owning_domain": map[string]any{
				"type": "string",
				"enum": []string{"network", "database", "application", "security"},
			},
			"contradictions": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Direct conflicts between specialist claims, and how you resolved each.",
			},
			"next_steps": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"diagnostic_queries": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Read-only commands for a human to run. Never propose a mutation.",
			},
			"cited_chunk_ids": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required":             []string{"consensus", "classification", "owning_domain", "contradictions", "next_steps", "diagnostic_queries", "cited_chunk_ids"},
		"additionalProperties": false,
	}
}
