package repository

import (
	"context"
	"encoding/json"
)

// DiagnosticTool defines the read-only contract for telemetry and cluster inspection adapters (MCP).
// In accordance with our Zero Blast Radius mandate, implementations must be strictly read-only
// and never execute mutations or configuration changes.
type DiagnosticTool interface {
	// Name returns the unique tool identifier (e.g. "prometheus_instant_query", "cloudwatch_logs_tail").
	Name() string

	// Description explains the diagnostic scope of this tool to LLM personas.
	Description() string

	// Execute runs the read-only diagnostic query within the caller's context timeout (<5s).
	Execute(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
}
