package notify

import "net/http"

// SlackForTest builds a Slack notifier against an arbitrary URL, bypassing the
// https and host checks so delivery behaviour can be tested against httptest.
func SlackForTest(rawURL string) *Slack {
	return &Slack{client: &http.Client{}, webhookURL: rawURL}
}

// DiscordForTest is the Discord equivalent of SlackForTest.
func DiscordForTest(rawURL string) *Discord {
	return &Discord{client: &http.Client{}, webhookURL: rawURL}
}
