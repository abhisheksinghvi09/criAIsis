package notify

import (
	"context"
	"net/http"
)

const (
	// discordFieldLimit is the per-field character ceiling Discord enforces.
	discordFieldLimit = 1024

	// discordDescriptionLimit is the embed description ceiling.
	discordDescriptionLimit = 4096

	// discordMaxFields is the number of fields one embed may carry.
	discordMaxFields = 25
)

// Discord posts an investigation to a Discord webhook.
type Discord struct {
	client     *http.Client
	webhookURL string
}

var _ Notifier = (*Discord)(nil)

// NewDiscord validates the webhook URL and builds the notifier.
func NewDiscord(webhookURL string) (*Discord, error) {
	if err := validateWebhookURL(webhookURL, "discord.com"); err != nil {
		// discordapp.com is the legacy host and still issues working webhooks.
		if legacyErr := validateWebhookURL(webhookURL, "discordapp.com"); legacyErr != nil {
			return nil, err
		}
	}
	return &Discord{client: noRedirectClient(), webhookURL: webhookURL}, nil
}

// Notify posts the report as a single embed with a severity-coloured stripe.
func (d *Discord) Notify(ctx context.Context, report Report) error {
	_, color := severityColor(report.Severity)

	fields := []any{}
	fields = appendDiscordField(fields, "Contradictions resolved", bulletList(report.Contradictions))
	fields = appendDiscordField(fields, "Next steps", bulletList(report.NextSteps))
	fields = appendDiscordField(fields, "Run these (read-only)", codeList(report.Queries))
	fields = appendDiscordField(fields, "Specialist positions", specialistList(report.Specialists))
	fields = appendDiscordField(fields, "Cited runbooks", bulletList(report.Citations))

	if len(fields) > discordMaxFields {
		fields = fields[:discordMaxFields]
	}

	return post(ctx, d.client, d.webhookURL, map[string]any{
		"embeds": []any{map[string]any{
			"title":       truncate(report.IncidentTitle, 256),
			"description": truncate("**"+report.headline()+"**\n\n"+report.Consensus, discordDescriptionLimit),
			"color":       color,
			"fields":      fields,
			"footer": map[string]any{
				"text": truncate("Investigated in "+report.Elapsed+". criAIsis is read-only and has changed nothing.", 2048),
			},
		}},
	})
}

// appendDiscordField adds a field when it has content.
func appendDiscordField(fields []any, name, value string) []any {
	if value == "" {
		return fields
	}
	return append(fields, map[string]any{
		"name":   name,
		"value":  truncate(value, discordFieldLimit),
		"inline": false,
	})
}

// bulletList renders items as a markdown list.
func bulletList(items []string) string {
	var out string
	for _, item := range items {
		out += "• " + item + "\n"
	}
	return out
}

// codeList renders commands in a fenced block so they copy cleanly.
func codeList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	var out string
	for _, item := range items {
		out += item + "\n"
	}
	return "```\n" + out + "```"
}

// specialistList renders every domain's position, failures included.
func specialistList(specialists []SpecialistSummary) string {
	var out string
	for _, s := range specialists {
		if s.Failed {
			out += "• **" + s.Persona + "**: no hypothesis (" + s.FailReason + ")\n"
			continue
		}
		out += "• **" + s.Persona + "** (" + s.Confidence + ownershipNote(s.OwnsThis) + "): " + s.Hypothesis + "\n"
	}
	return out
}
