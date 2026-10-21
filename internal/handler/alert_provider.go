package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"criaisis/internal/domain/value"
)

// ParsedAlert is the provider-neutral result of decoding a monitoring webhook.
type ParsedAlert struct {
	Title       string
	Description string
	Severity    value.Severity
	Context     value.IncidentContext
}

// ParseAlert decodes a provider payload into the shape the clash engine consumes.
func ParseAlert(provider string, body []byte) (*ParsedAlert, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "grafana":
		return parseGrafana(body)
	case "cloudwatch":
		return parseCloudWatch(body)
	default:
		return nil, fmt.Errorf("unsupported alert provider %q", provider)
	}
}

// grafanaPayload covers the fields of Grafana unified alerting this product uses.
type grafanaPayload struct {
	Title        string            `json:"title"`
	Message      string            `json:"message"`
	Status       string            `json:"status"`
	CommonLabels map[string]string `json:"commonLabels"`
	Alerts       []struct {
		Status      string             `json:"status"`
		Labels      map[string]string  `json:"labels"`
		Annotations map[string]string  `json:"annotations"`
		ValueString string             `json:"valueString"`
		Values      map[string]float64 `json:"values"`
	} `json:"alerts"`
}

// parseGrafana flattens a Grafana alert group into one incident.
func parseGrafana(body []byte) (*ParsedAlert, error) {
	var payload grafanaPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decoding grafana payload: %w", err)
	}
	if len(payload.Alerts) == 0 {
		return nil, fmt.Errorf("grafana payload contains no alerts")
	}

	var (
		logs    []string
		metrics = map[string]float64{}
	)
	for _, alert := range payload.Alerts {
		if summary := alert.Annotations["summary"]; summary != "" {
			logs = append(logs, summary)
		}
		if desc := alert.Annotations["description"]; desc != "" {
			logs = append(logs, desc)
		}
		if alert.ValueString != "" {
			logs = append(logs, alert.ValueString)
		}
		for name, v := range alert.Values {
			metrics[name] = v
		}
	}

	alertName := firstNonEmpty(payload.CommonLabels["alertname"], payload.Alerts[0].Labels["alertname"], payload.Title)
	title := firstNonEmpty(payload.Title, alertName, "Grafana alert")

	return &ParsedAlert{
		Title:       title,
		Description: firstNonEmpty(payload.Message, payload.Alerts[0].Annotations["description"], title),
		Severity:    severityFromLabel(payload.CommonLabels["severity"], payload.Alerts[0].Labels["severity"]),
		Context: value.NewIncidentContext(
			"grafana", alertName, logs, nil, metrics,
		),
	}, nil
}

// snsEnvelope is the SNS wrapper CloudWatch alarms are delivered inside. The alarm
// itself arrives as a JSON string in Message, so it needs a second decode.
type snsEnvelope struct {
	Type         string `json:"Type"`
	Subject      string `json:"Subject"`
	Message      string `json:"Message"`
	SubscribeURL string `json:"SubscribeURL"`
	TopicArn     string `json:"TopicArn"`
}

// ErrSNSConfirmation signals the subscription handshake rather than an alert.
// SNS will not deliver a single alarm until this is completed, so it must be
// handled rather than rejected as a malformed payload.
var ErrSNSConfirmation = errors.New("sns subscription confirmation")

// SNSConfirmation carries the URL that must be fetched to activate a subscription.
type SNSConfirmation struct {
	SubscribeURL string
	TopicArn     string
}

// DetectSNSConfirmation reports whether a body is an SNS subscription handshake.
func DetectSNSConfirmation(body []byte) (*SNSConfirmation, bool) {
	var envelope snsEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false
	}
	if envelope.Type != "SubscriptionConfirmation" || envelope.SubscribeURL == "" {
		return nil, false
	}
	return &SNSConfirmation{SubscribeURL: envelope.SubscribeURL, TopicArn: envelope.TopicArn}, true
}

// cloudWatchAlarm is the alarm body CloudWatch publishes to SNS.
type cloudWatchAlarm struct {
	AlarmName        string `json:"AlarmName"`
	AlarmDescription string `json:"AlarmDescription"`
	NewStateValue    string `json:"NewStateValue"`
	NewStateReason   string `json:"NewStateReason"`
	Region           string `json:"Region"`
	Trigger          struct {
		MetricName string  `json:"MetricName"`
		Namespace  string  `json:"Namespace"`
		Threshold  float64 `json:"Threshold"`
		Period     int     `json:"Period"`
	} `json:"Trigger"`
}

// parseCloudWatch handles both the SNS-wrapped and the bare alarm shapes, because
// which one arrives depends on how the operator wired the subscription.
func parseCloudWatch(body []byte) (*ParsedAlert, error) {
	raw := body

	var envelope snsEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Type == "SubscriptionConfirmation" {
			return nil, ErrSNSConfirmation
		}
		if envelope.Message != "" {
			raw = []byte(envelope.Message)
		}
	}

	var alarm cloudWatchAlarm
	if err := json.Unmarshal(raw, &alarm); err != nil {
		return nil, fmt.Errorf("decoding cloudwatch alarm: %w", err)
	}
	if strings.TrimSpace(alarm.AlarmName) == "" {
		return nil, fmt.Errorf("cloudwatch payload has no alarm name")
	}

	logs := []string{}
	if alarm.NewStateReason != "" {
		logs = append(logs, alarm.NewStateReason)
	}
	if alarm.Region != "" {
		logs = append(logs, "region="+alarm.Region)
	}

	metrics := map[string]float64{}
	if alarm.Trigger.MetricName != "" {
		metrics[alarm.Trigger.MetricName+"_threshold"] = alarm.Trigger.Threshold
	}

	return &ParsedAlert{
		Title:       alarm.AlarmName,
		Description: firstNonEmpty(alarm.AlarmDescription, alarm.NewStateReason, alarm.AlarmName),
		Severity:    severityFromCloudWatchState(alarm.NewStateValue),
		Context: value.NewIncidentContext(
			"cloudwatch", alarm.AlarmName, logs, nil, metrics,
		),
	}, nil
}

// severityFromLabel maps a provider severity label onto the native tier, defaulting
// to sev-2 so an unlabelled alert is still treated as a real incident.
func severityFromLabel(labels ...string) value.Severity {
	for _, label := range labels {
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "critical", "crit", "page", "sev-1", "sev1":
			return value.SeveritySev1
		case "error", "high", "major", "sev-2", "sev2":
			return value.SeveritySev2
		case "warning", "warn", "sev-3", "sev3":
			return value.SeveritySev3
		case "info", "low", "sev-4", "sev4":
			return value.SeveritySev4
		}
	}
	return value.SeveritySev2
}

// severityFromCloudWatchState maps alarm state onto a severity tier.
func severityFromCloudWatchState(state string) value.Severity {
	if strings.EqualFold(strings.TrimSpace(state), "ALARM") {
		return value.SeveritySev2
	}
	return value.SeveritySev3
}

// firstNonEmpty returns the first argument that has content after trimming.
func firstNonEmpty(candidates ...string) string {
	for _, candidate := range candidates {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
