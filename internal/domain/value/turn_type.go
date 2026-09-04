package value

import (
	"fmt"
	"strings"
)

// TurnType defines the conversational role and formatting structure of a debate entry.
type TurnType string

const (
	// TurnTypeSpecialistHypothesis is authored by an individual persona grounded in its runbooks.
	TurnTypeSpecialistHypothesis TurnType = "specialist_hypothesis"

	// TurnTypeSynthesis is authored by the orchestrator contrasting specialist claims into a consensus verdict.
	TurnTypeSynthesis TurnType = "synthesis"

	// TurnTypeFollowUp is an ad-hoc direct response to an engineer's @mention query in Slack.
	TurnTypeFollowUp TurnType = "follow_up"
)

// ParseTurnType validates turn classification from incoming event streams or DB rows.
func ParseTurnType(raw string) (TurnType, error) {
	canonical := strings.ToLower(strings.TrimSpace(raw))
	switch canonical {
	case "specialist_hypothesis":
		return TurnTypeSpecialistHypothesis, nil
	case "synthesis":
		return TurnTypeSynthesis, nil
	case "follow_up":
		return TurnTypeFollowUp, nil
	default:
		return "", fmt.Errorf("invalid turn type '%s': expected specialist_hypothesis, synthesis, or follow_up", raw)
	}
}

// String returns the raw string identifier.
func (t TurnType) String() string {
	return string(t)
}
