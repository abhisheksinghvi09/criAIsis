package value

import (
	"fmt"
	"strings"
)

// IssueClassification categorizes the diagnosed failure mode identified during Stage 2 consensus synthesis.
// Classifying the failure into Code, Infra, or Hybrid immediately routes resolution to the responsible engineering team.
type IssueClassification string

const (
	// IssueClassificationCode: Software defect, unhandled exception, breaking API payload, or bad migration script.
	IssueClassificationCode IssueClassification = "code"

	// IssueClassificationInfra: Hardware failure, cloud provider AZ impairment, node disk pressure, or network packet loss.
	IssueClassificationInfra IssueClassification = "infra"

	// IssueClassificationHybrid: Code deployment that triggered an infrastructure collapse (e.g. unindexed query exhausting connection pool).
	IssueClassificationHybrid IssueClassification = "hybrid"
)

// ParseIssueClassification validates incoming classification strings from the orchestrator synthesis output.
func ParseIssueClassification(raw string) (IssueClassification, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "code", "code_level", "software":
		return IssueClassificationCode, nil
	case "infra", "infrastructure", "hardware":
		return IssueClassificationInfra, nil
	case "hybrid", "both", "code_and_infra":
		return IssueClassificationHybrid, nil
	default:
		return "", fmt.Errorf("invalid issue classification '%s': expected 'code', 'infra', or 'hybrid'", raw)
	}
}

// String returns the canonical classification string.
func (c IssueClassification) String() string {
	return string(c)
}

// IsValid checks whether the classification is one of the three recognized failure modes.
func (c IssueClassification) IsValid() bool {
	switch c {
	case IssueClassificationCode, IssueClassificationInfra, IssueClassificationHybrid:
		return true
	default:
		return false
	}
}
