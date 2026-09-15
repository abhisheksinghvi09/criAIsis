package value

import (
	"fmt"
	"strings"
)

// TriggerType models how an incident was initiated (manual slash command vs automated webhook).
type TriggerType string

const (
	TriggerTypeSlashCommand TriggerType = "slash_command"
	TriggerTypeWebhook      TriggerType = "webhook"
)

// ParseTriggerType validates trigger type string representations from Slack slash commands or webhooks.
func ParseTriggerType(raw string) (TriggerType, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "slash_command":
		return TriggerTypeSlashCommand, nil
	case "webhook":
		return TriggerTypeWebhook, nil
	default:
		return "", fmt.Errorf("invalid trigger type '%s': must be 'slash_command' or 'webhook'", raw)
	}
}

// String returns the raw string representation.
func (t TriggerType) String() string {
	return string(t)
}

// IsValid returns true if the trigger type is recognized.
func (t TriggerType) IsValid() bool {
	return t == TriggerTypeSlashCommand || t == TriggerTypeWebhook
}

// IsWebhook returns true if the incident was triggered automatically by an alert webhook.
func (t TriggerType) IsWebhook() bool {
	return t == TriggerTypeWebhook
}
