package handler_test

import (
	"testing"

	"criaisis/internal/domain/value"
	"criaisis/internal/handler"
)

const grafanaPayload = `{
  "title": "[FIRING:1] CheckoutHighLatency",
  "message": "p99 latency above threshold on checkout-api",
  "status": "firing",
  "commonLabels": {"alertname": "CheckoutHighLatency", "severity": "critical"},
  "alerts": [{
    "status": "firing",
    "labels": {"alertname": "CheckoutHighLatency", "service": "checkout-api"},
    "annotations": {"summary": "p99 5200ms", "description": "pgx pool acquire timeout after 5000ms"},
    "valueString": "[ var='B' value=5200 ]",
    "values": {"p99_latency_ms": 5200}
  }]
}`

func TestParseAlert_Grafana(t *testing.T) {
	alert, err := handler.ParseAlert("grafana", []byte(grafanaPayload))
	if err != nil {
		t.Fatalf("parsing grafana payload: %v", err)
	}

	if alert.Severity != value.SeveritySev1 {
		t.Errorf("expected critical to map to sev-1, got %s", alert.Severity)
	}
	if alert.Context.Provider() != "grafana" {
		t.Errorf("expected grafana provider, got %s", alert.Context.Provider())
	}
	if alert.Context.AlertName() != "CheckoutHighLatency" {
		t.Errorf("unexpected alert name %q", alert.Context.AlertName())
	}
	if len(alert.Context.ErrorLogs()) < 2 {
		t.Errorf("expected summary and description to become evidence, got %d entries", len(alert.Context.ErrorLogs()))
	}
	if got := alert.Context.Metrics()["p99_latency_ms"]; got != 5200 {
		t.Errorf("expected metric to survive parsing, got %v", got)
	}
}

const cloudWatchSNS = `{
  "Type": "Notification",
  "Subject": "ALARM: DBConnectionsHigh",
  "Message": "{\"AlarmName\":\"DBConnectionsHigh\",\"AlarmDescription\":\"Connections near max\",\"NewStateValue\":\"ALARM\",\"NewStateReason\":\"Threshold crossed: 200 > 190\",\"Region\":\"eu-west-1\",\"Trigger\":{\"MetricName\":\"DatabaseConnections\",\"Namespace\":\"AWS/RDS\",\"Threshold\":190}}"
}`

func TestParseAlert_CloudWatchViaSNS(t *testing.T) {
	alert, err := handler.ParseAlert("cloudwatch", []byte(cloudWatchSNS))
	if err != nil {
		t.Fatalf("parsing cloudwatch payload: %v", err)
	}

	if alert.Title != "DBConnectionsHigh" {
		t.Errorf("unexpected title %q", alert.Title)
	}
	if alert.Severity != value.SeveritySev2 {
		t.Errorf("expected ALARM to map to sev-2, got %s", alert.Severity)
	}
	if got := alert.Context.Metrics()["DatabaseConnections_threshold"]; got != 190 {
		t.Errorf("expected threshold metric, got %v", got)
	}
	if len(alert.Context.ErrorLogs()) == 0 {
		t.Error("expected the state reason to become evidence")
	}
}

// Operators wire CloudWatch either through SNS or directly, so both shapes must work.
func TestParseAlert_CloudWatchBareAlarm(t *testing.T) {
	bare := `{"AlarmName":"DiskPressure","NewStateValue":"ALARM","NewStateReason":"disk 94%"}`

	alert, err := handler.ParseAlert("cloudwatch", []byte(bare))
	if err != nil {
		t.Fatalf("parsing bare alarm: %v", err)
	}
	if alert.Title != "DiskPressure" {
		t.Errorf("unexpected title %q", alert.Title)
	}
}

func TestParseAlert_RejectsUnknownProvider(t *testing.T) {
	if _, err := handler.ParseAlert("pagerduty", []byte(`{}`)); err == nil {
		t.Fatal("expected an unsupported provider to be rejected")
	}
}

func TestParseAlert_RejectsMalformedPayloads(t *testing.T) {
	cases := map[string]struct{ provider, body string }{
		"grafana with no alerts": {"grafana", `{"title":"x","alerts":[]}`},
		"grafana not json":       {"grafana", `not json`},
		"cloudwatch no name":     {"cloudwatch", `{"NewStateValue":"ALARM"}`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := handler.ParseAlert(tc.provider, []byte(tc.body)); err == nil {
				t.Error("expected a parse error")
			}
		})
	}
}

// An unlabelled alert is still a real incident: it must not silently become the
// lowest tier and get ignored.
func TestParseAlert_DefaultsSeverityWhenUnlabelled(t *testing.T) {
	alert, err := handler.ParseAlert("grafana", []byte(`{"title":"Unlabelled","alerts":[{"labels":{},"annotations":{"summary":"something broke"}}]}`))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if alert.Severity != value.SeveritySev2 {
		t.Errorf("expected sev-2 default, got %s", alert.Severity)
	}
}
