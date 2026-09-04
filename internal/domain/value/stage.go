package value

import (
	"fmt"
)

// Stage partitions debate messages according to the 2-Stage Asynchronous Clash engine.
type Stage int

const (
	// StageSpecialistBlast: 4 specialist agents execute concurrent RAG and formulate initial domain hypotheses.
	StageSpecialistBlast Stage = 1

	// StageConsensusSynthesis: The orchestrator cross-examines hypotheses and resolves contradictions into IC consensus.
	StageConsensusSynthesis Stage = 2

	// StageInteractiveFollowUp: Human commanders query specific agents in-thread via @mentions.
	StageInteractiveFollowUp Stage = 3
)

// ParseStage guarantees only known stages enter the domain model.
func ParseStage(s int) (Stage, error) {
	if s < 1 || s > 3 {
		return 0, fmt.Errorf("invalid clash stage %d: must be 1 (specialist), 2 (synthesis), or 3 (follow-up)", s)
	}
	return Stage(s), nil
}

// Int returns the integer representation for SQL columns.
func (s Stage) Int() int {
	return int(s)
}
