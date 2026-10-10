// Package notify delivers a finished investigation to the channel the team is
// already watching. Without it the clash engine writes a verdict nobody reads.
package notify

import (
	"context"
	"fmt"
	"strings"
)

// Provider identifies a supported destination.
type Provider string

const (
	// ProviderNone disables delivery for a workspace.
	ProviderNone Provider = "none"

	// ProviderSlack posts through a Slack incoming webhook.
	ProviderSlack Provider = "slack"

	// ProviderDiscord posts through a Discord webhook.
	ProviderDiscord Provider = "discord"
)

// ParseProvider validates a configured destination.
func ParseProvider(raw string) (Provider, error) {
	switch Provider(strings.ToLower(strings.TrimSpace(raw))) {
	case ProviderNone, "":
		return ProviderNone, nil
	case ProviderSlack:
		return ProviderSlack, nil
	case ProviderDiscord:
		return ProviderDiscord, nil
	default:
		return "", fmt.Errorf("unsupported notification provider %q: expected none, slack or discord", raw)
	}
}

// String returns the canonical provider name.
func (p Provider) String() string { return string(p) }

// Notifier delivers one finished investigation.
type Notifier interface {
	Notify(ctx context.Context, report Report) error
}

// New builds the notifier for a workspace's configured destination.
func New(provider Provider, webhookURL string) (Notifier, error) {
	switch provider {
	case ProviderSlack:
		return NewSlack(webhookURL)
	case ProviderDiscord:
		return NewDiscord(webhookURL)
	case ProviderNone, "":
		return Discard{}, nil
	default:
		return nil, fmt.Errorf("unsupported notification provider %q", provider)
	}
}

// Discard drops reports. It exists so a workspace with no destination configured
// follows the same code path as one that has, rather than a nil check at the call site.
type Discard struct{}

// Notify does nothing and succeeds.
func (Discard) Notify(context.Context, Report) error { return nil }
