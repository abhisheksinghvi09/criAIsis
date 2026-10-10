package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PrometheusTool exposes the Prometheus instant query endpoint as a read-only probe.
//
// Only /api/v1/query is reachable through this adapter, and only over GET. The
// admin and TSDB-mutating endpoints are unreachable by construction, not by policy.
type PrometheusTool struct {
	client  *http.Client
	baseURL string
}

var _ repositoryDiagnosticTool = (*PrometheusTool)(nil)

// repositoryDiagnosticTool mirrors repository.DiagnosticTool for the compile-time
// assertion without importing the domain package into every adapter file.
type repositoryDiagnosticTool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
}

// NewPrometheusTool constructs a read-only Prometheus adapter. The caller's registry
// enforces the timeout, so the HTTP client does not impose a second, conflicting one.
func NewPrometheusTool(baseURL string) *PrometheusTool {
	return &PrometheusTool{
		client:  &http.Client{},
		baseURL: strings.TrimSuffix(baseURL, "/"),
	}
}

// Name identifies the tool to personas and the registry.
func (p *PrometheusTool) Name() string { return "prometheus_instant_query" }

// Description tells a persona what this probe can answer.
func (p *PrometheusTool) Description() string {
	return `Evaluate a PromQL expression at the current instant. Params: {"query":"<promql>"}. Read-only.`
}

type prometheusParams struct {
	Query string `json:"query"`
}

// Execute evaluates a PromQL expression and returns the raw Prometheus result payload.
func (p *PrometheusTool) Execute(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var parsed prometheusParams
	if err := json.Unmarshal(params, &parsed); err != nil {
		return nil, fmt.Errorf("parsing params: %w", err)
	}
	if strings.TrimSpace(parsed.Query) == "" {
		return nil, fmt.Errorf("query parameter is required")
	}

	endpoint := p.baseURL + "/api/v1/query?" + url.Values{"query": {parsed.Query}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying prometheus: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned status %d", resp.StatusCode)
	}
	return json.RawMessage(body), nil
}
