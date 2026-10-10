package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"criaisis/internal/infrastructure/llm"
)

// chunkIDPattern recovers the excerpt IDs the prompt offered, so stub citations
// exercise the same filtering path a real model's citations travel.
var chunkIDPattern = regexp.MustCompile(`\[chunk_id: ([0-9a-fA-F-]{36})\]`)

// NewStubChat returns a deterministic ChatProvider for offline simulation runs.
//
// It performs NO diagnosis. It reflects the prompt back in the right schema so the
// pipeline, retrieval scoping, citation filtering, and persistence can be exercised
// without API spend. Its classification is a constant, so offline simulation must
// never assert classification accuracy: that assertion needs a real model.
func NewStubChat() llm.ChatProvider {
	return llm.ChatFunc(func(_ context.Context, req llm.ChatRequest) (string, error) {
		if req.Role == llm.RoleSynthesis {
			return marshal(stubSynthesis(req.User))
		}
		return marshal(stubSpecialist(req.User))
	})
}

// stubSpecialist fabricates a schema-valid Stage 1 hypothesis citing every excerpt
// the prompt showed it.
func stubSpecialist(prompt string) SpecialistResult {
	ids := chunkIDsIn(prompt)
	return SpecialistResult{
		Hypothesis:        fmt.Sprintf("stub hypothesis grounded in %d runbook excerpt(s)", len(ids)),
		Confidence:        confidenceFor(len(ids)),
		OwnsThis:          len(ids) > 0,
		Evidence:          []string{"stub evidence: see incident telemetry above"},
		CitedChunkIDs:     ids,
		DiagnosticQueries: []string{"stub diagnostic query"},
	}
}

// stubSynthesis fabricates a schema-valid Stage 2 verdict.
func stubSynthesis(prompt string) SynthesisResult {
	return SynthesisResult{
		Consensus:      "stub consensus: offline simulation does not perform diagnosis",
		Classification: "hybrid", // constant on purpose; never assert on this offline
		OwningDomain:   "application",
		Contradictions: []string{"stub contradiction"},
		NextSteps:      []string{"stub next step"},
		// Citing the Stage 1 IDs verifies that synthesis citations survive pooling.
		DiagnosticQueries: []string{"stub diagnostic query"},
		CitedChunkIDs:     chunkIDsIn(prompt),
	}
}

// confidenceFor reports low confidence when nothing was retrieved, mirroring the
// "admit the gap" instruction real personas are given.
func confidenceFor(citations int) string {
	if citations == 0 {
		return "none"
	}
	return "medium"
}

// chunkIDsIn extracts the excerpt IDs a prompt exposed.
func chunkIDsIn(prompt string) []string {
	matches := chunkIDPattern.FindAllStringSubmatch(prompt, -1)
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match[1])
	}
	return ids
}

// marshal renders a stub result as the JSON the orchestrator expects to parse.
func marshal(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("stub chat: %w", err)
	}
	return string(raw), nil
}
