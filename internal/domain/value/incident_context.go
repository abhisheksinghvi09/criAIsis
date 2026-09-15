package value

import (
	"encoding/json"
	"strings"
)

// IncidentContext encapsulates incoming telemetry evidence (error logs, stack traces, metric spikes)
// parsed from Grafana, CloudWatch, or user inputs.
type IncidentContext struct {
	provider    string
	alertName   string
	errorLogs   []string
	stackTraces []string
	metrics     map[string]float64
}

// NewIncidentContext constructs an IncidentContext with defensive copying.
func NewIncidentContext(
	provider string,
	alertName string,
	errorLogs []string,
	stackTraces []string,
	metrics map[string]float64,
) IncidentContext {
	trimmedProvider := strings.TrimSpace(strings.ToLower(provider))
	if trimmedProvider == "" {
		trimmedProvider = "manual"
	}

	copiedLogs := make([]string, len(errorLogs))
	copy(copiedLogs, errorLogs)

	copiedTraces := make([]string, len(stackTraces))
	copy(copiedTraces, stackTraces)

	copiedMetrics := make(map[string]float64, len(metrics))
	for k, v := range metrics {
		copiedMetrics[k] = v
	}

	return IncidentContext{
		provider:    trimmedProvider,
		alertName:   strings.TrimSpace(alertName),
		errorLogs:   copiedLogs,
		stackTraces: copiedTraces,
		metrics:     copiedMetrics,
	}
}

// Provider returns the source monitoring system or "manual".
func (c IncidentContext) Provider() string {
	return c.provider
}

// AlertName returns the alert name or rule identifier.
func (c IncidentContext) AlertName() string {
	return c.alertName
}

// ErrorLogs returns a defensive copy of the ingested error log excerpts.
func (c IncidentContext) ErrorLogs() []string {
	cp := make([]string, len(c.errorLogs))
	copy(cp, c.errorLogs)
	return cp
}

// StackTraces returns a defensive copy of the ingested exception stack traces.
func (c IncidentContext) StackTraces() []string {
	cp := make([]string, len(c.stackTraces))
	copy(cp, c.stackTraces)
	return cp
}

// Metrics returns a defensive copy of key metric values captured at alert time.
func (c IncidentContext) Metrics() map[string]float64 {
	cp := make(map[string]float64, len(c.metrics))
	for k, v := range c.metrics {
		cp[k] = v
	}
	return cp
}

// IsEmpty returns true if there is no telemetry evidence attached.
func (c IncidentContext) IsEmpty() bool {
	return len(c.errorLogs) == 0 && len(c.stackTraces) == 0 && len(c.metrics) == 0
}

type incidentContextJSON struct {
	Provider    string             `json:"provider"`
	AlertName   string             `json:"alert_name,omitempty"`
	ErrorLogs   []string           `json:"error_logs,omitempty"`
	StackTraces []string           `json:"stack_traces,omitempty"`
	Metrics     map[string]float64 `json:"metrics,omitempty"`
}

// MarshalJSON provides deterministic JSON encoding.
func (c IncidentContext) MarshalJSON() ([]byte, error) {
	return json.Marshal(incidentContextJSON{
		Provider:    c.provider,
		AlertName:   c.alertName,
		ErrorLogs:   c.errorLogs,
		StackTraces: c.stackTraces,
		Metrics:     c.metrics,
	})
}

// UnmarshalJSON deserializes and validates IncidentContext.
func (c *IncidentContext) UnmarshalJSON(data []byte) error {
	var aux incidentContextJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = NewIncidentContext(aux.Provider, aux.AlertName, aux.ErrorLogs, aux.StackTraces, aux.Metrics)
	return nil
}
