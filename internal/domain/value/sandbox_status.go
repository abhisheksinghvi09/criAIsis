package value

import (
	"fmt"
	"strings"
)

// SandboxStatus models the lifecycle of a sandbox reproduction attempt.
type SandboxStatus string

const (
	SandboxStatusProvisioning      SandboxStatus = "Provisioning"
	SandboxStatusReady             SandboxStatus = "Ready"
	SandboxStatusFailed            SandboxStatus = "Failed"
	SandboxStatusUnmatchedFallback SandboxStatus = "UnmatchedFallback"
)

// ParseSandboxStatus parses and validates a raw status string.
func ParseSandboxStatus(raw string) (SandboxStatus, error) {
	switch strings.TrimSpace(raw) {
	case string(SandboxStatusProvisioning):
		return SandboxStatusProvisioning, nil
	case string(SandboxStatusReady):
		return SandboxStatusReady, nil
	case string(SandboxStatusFailed):
		return SandboxStatusFailed, nil
	case string(SandboxStatusUnmatchedFallback):
		return SandboxStatusUnmatchedFallback, nil
	default:
		return "", fmt.Errorf("invalid sandbox status '%s'", raw)
	}
}

func (s SandboxStatus) String() string {
	return string(s)
}

func (s SandboxStatus) IsTerminal() bool {
	return s == SandboxStatusReady || s == SandboxStatusFailed
}
