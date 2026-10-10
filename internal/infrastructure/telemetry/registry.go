// Package telemetry hosts read-only diagnostic tool adapters (MCP) that specialist
// personas may probe during Stage 1.
//
// Every adapter here is bound by the Zero Blast Radius mandate: read-only queries
// only, hard timeouts, and no mutation surface anywhere in the contract.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"criaisis/internal/domain/repository"
)

// ToolTimeout is the hard ceiling on any single diagnostic probe. An incident
// hypothesis is due in seconds, so a slow telemetry backend must be abandoned
// rather than allowed to blow the stage budget.
const ToolTimeout = 5 * time.Second

// Registry resolves tool names to read-only adapters and enforces the probe timeout
// on their behalf, so no individual adapter can opt out of it.
type Registry struct {
	tools map[string]repository.DiagnosticTool
}

// NewRegistry indexes adapters by name. Later registrations of the same name win,
// which lets a deployment override a built-in adapter.
func NewRegistry(tools ...repository.DiagnosticTool) *Registry {
	indexed := make(map[string]repository.DiagnosticTool, len(tools))
	for _, tool := range tools {
		indexed[tool.Name()] = tool
	}
	return &Registry{tools: indexed}
}

// Names lists the registered tools in stable order for prompt construction.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Describe renders the catalogue of available probes for a persona prompt.
func (r *Registry) Describe() string {
	var out string
	for _, name := range r.Names() {
		out += fmt.Sprintf("- %s: %s\n", name, r.tools[name].Description())
	}
	return out
}

// Execute runs a named read-only probe under ToolTimeout.
//
// The caller's context still bounds the call, so a cancelled incident abandons
// in-flight probes immediately.
func (r *Registry) Execute(ctx context.Context, name string, params json.RawMessage) (json.RawMessage, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown diagnostic tool %q", name)
	}

	ctx, cancel := context.WithTimeout(ctx, ToolTimeout)
	defer cancel()

	result, err := tool.Execute(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("diagnostic tool %q: %w", name, err)
	}
	return result, nil
}

// Probe runs a tool and degrades gracefully: an unreachable or slow telemetry
// backend yields a note for the prompt rather than failing the specialist turn.
func (r *Registry) Probe(ctx context.Context, name string, params json.RawMessage) string {
	result, err := r.Execute(ctx, name, params)
	if err != nil {
		return fmt.Sprintf("%s: unavailable (%v)", name, err)
	}
	return fmt.Sprintf("%s: %s", name, string(result))
}
