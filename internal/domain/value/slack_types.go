package value

import (
	"fmt"
	"strings"
)

// SlackChannelID strongly types a Slack channel identifier (e.g. C01234567).
type SlackChannelID string

func NewSlackChannelID(raw string) (SlackChannelID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("slack channel id cannot be empty")
	}
	return SlackChannelID(trimmed), nil
}

func (c SlackChannelID) String() string {
	return string(c)
}

// SlackThreadTS strongly types a Slack message/thread timestamp (e.g. 1709512345.000200).
// In Slack's architecture, thread_ts serves as the immutable parent anchor for threaded discussions.
type SlackThreadTS string

func NewSlackThreadTS(raw string) (SlackThreadTS, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("slack thread ts cannot be empty")
	}
	return SlackThreadTS(trimmed), nil
}

func (t SlackThreadTS) String() string {
	return string(t)
}

// SlackUserID strongly types a Slack member identifier (e.g. U01234567).
type SlackUserID string

func NewSlackUserID(raw string) (SlackUserID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("slack user id cannot be empty")
	}
	return SlackUserID(trimmed), nil
}

func (u SlackUserID) String() string {
	return string(u)
}
