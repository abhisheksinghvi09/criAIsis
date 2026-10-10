package notify

// SlackForTest builds a Slack notifier against an arbitrary URL, bypassing the
// https and host checks so delivery behaviour can be tested against httptest.
// It still uses the production no-redirect client, so redirect handling is
// exercised exactly as it runs for real.
func SlackForTest(rawURL string) *Slack {
	return &Slack{client: noRedirectClient(), webhookURL: rawURL}
}

// DiscordForTest is the Discord equivalent of SlackForTest.
func DiscordForTest(rawURL string) *Discord {
	return &Discord{client: noRedirectClient(), webhookURL: rawURL}
}
