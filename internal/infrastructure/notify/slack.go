package notify

import (
	"context"
	"net/http"
)

// slackTextLimit is the per-block character ceiling the Slack API enforces.
const slackTextLimit = 2900

// Slack posts an investigation to a Slack incoming webhook.
//
// An incoming webhook is used rather than the Web API because it needs no OAuth
// install, no scopes and no app review: one URL and the verdict lands in a channel.
type Slack struct {
	client     *http.Client
	webhookURL string
}

var _ Notifier = (*Slack)(nil)

// NewSlack validates the webhook URL and builds the notifier.
func NewSlack(webhookURL string) (*Slack, error) {
	if err := validateWebhookURL(webhookURL, "slack.com"); err != nil {
		return nil, err
	}
	return &Slack{client: noRedirectClient(), webhookURL: webhookURL}, nil
}

// Notify posts the report as a Block Kit attachment with a severity stripe.
func (s *Slack) Notify(ctx context.Context, report Report) error {
	color, _ := severityColor(report.Severity)

	blocks := []any{
		map[string]any{
			"type": "header",
			"text": map[string]any{"type": "plain_text", "text": truncate(report.IncidentTitle, 150), "emoji": false},
		},
		map[string]any{
			"type":     "context",
			"elements": []any{map[string]any{"type": "mrkdwn", "text": report.headline()}},
		},
		map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": truncate("*Consensus*\n"+report.Consensus, slackTextLimit)},
		},
	}

	blocks = appendSlackList(blocks, "Contradictions resolved", report.Contradictions)
	blocks = appendSlackList(blocks, "Next steps", report.NextSteps)
	blocks = appendSlackCode(blocks, "Run these (read-only)", report.Queries)
	blocks = appendSlackSpecialists(blocks, report.Specialists)
	blocks = appendSlackList(blocks, "Cited runbooks", report.Citations)

	blocks = append(blocks, map[string]any{
		"type": "context",
		"elements": []any{map[string]any{
			"type": "mrkdwn",
			"text": "Investigated in " + report.Elapsed + ". criAIsis is read-only and has changed nothing.",
		}},
	})

	return post(ctx, s.client, s.webhookURL, map[string]any{
		// text is the notification preview and the accessibility fallback.
		"text":        report.IncidentTitle + " — " + report.headline(),
		"attachments": []any{map[string]any{"color": color, "blocks": blocks}},
	})
}

// appendSlackList adds a bulleted section when it has content.
func appendSlackList(blocks []any, title string, items []string) []any {
	if len(items) == 0 {
		return blocks
	}
	var body string
	for _, item := range items {
		body += "• " + item + "\n"
	}
	return append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": truncate("*"+title+"*\n"+body, slackTextLimit)},
	})
}

// appendSlackCode renders commands in a code block so they can be copied cleanly.
func appendSlackCode(blocks []any, title string, items []string) []any {
	if len(items) == 0 {
		return blocks
	}
	var body string
	for _, item := range items {
		body += item + "\n"
	}
	return append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": truncate("*"+title+"*\n```\n"+body+"```", slackTextLimit)},
	})
}

// appendSlackSpecialists shows each domain's position, including the ones that failed.
func appendSlackSpecialists(blocks []any, specialists []SpecialistSummary) []any {
	if len(specialists) == 0 {
		return blocks
	}
	var body string
	for _, s := range specialists {
		if s.Failed {
			body += "• *" + s.Persona + "*: no hypothesis (" + s.FailReason + ")\n"
			continue
		}
		body += "• *" + s.Persona + "* (" + s.Confidence + ownershipNote(s.OwnsThis) + "): " + s.Hypothesis + "\n"
	}
	return append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": truncate("*Specialist positions*\n"+body, slackTextLimit)},
	})
}
