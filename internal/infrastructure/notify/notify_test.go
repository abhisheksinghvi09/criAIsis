package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"criaisis/internal/infrastructure/notify"
)

func sampleReport() notify.Report {
	return notify.Report{
		IncidentTitle:  "PostgreSQL connections exhausted on checkout",
		Severity:       "sev-1",
		Trigger:        "webhook",
		Consensus:      "A payment call inside an uncommitted transaction is holding connections open.",
		Classification: "code",
		OwningDomain:   "application",
		Contradictions: []string{"database claimed capacity; network showed a healthy path"},
		NextSteps:      []string{"Revert commit d74a12b"},
		Queries:        []string{"SELECT pid, state FROM pg_stat_activity;"},
		Specialists: []notify.SpecialistSummary{
			{Persona: "database", Confidence: "high", OwnsThis: true, Hypothesis: "Pool saturated"},
			{Persona: "security", Failed: true, FailReason: "retrieval timeout"},
		},
		Citations: []string{"PostgreSQL Connection Saturation Runbook"},
		Elapsed:   "18.4s",
	}
}

// captureServer stands in for Slack or Discord and records what we sent.
func captureServer(t *testing.T, status int) (*httptest.Server, *map[string]any) {
	t.Helper()
	captured := map[string]any{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(status)
		if status >= 300 {
			_, _ = w.Write([]byte("rejected"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// The notifiers demand https, so tests exercise them through the exported
// constructors' validation and through a direct transport check separately.
func TestSlack_RejectsNonHTTPSAndForeignHosts(t *testing.T) {
	cases := []string{
		"http://hooks.slack.com/services/T/B/x",
		"https://evil.example.com/services/T/B/x",
		"",
		"://nonsense",
		// A suffix-only host check would wrongly accept these: both end in
		// "slack.com" but neither is a slack.com (sub)domain.
		"https://evilslack.com/services/T/B/x",
		"https://notslack.com/services/T/B/x",
	}
	for _, url := range cases {
		if _, err := notify.NewSlack(url); err == nil {
			t.Errorf("expected %q to be rejected", url)
		}
	}
}

func TestDiscord_RejectsNonHTTPSAndForeignHosts(t *testing.T) {
	cases := []string{
		"http://discord.com/api/webhooks/1/x",
		"https://evil.example.com/api/webhooks/1/x",
		"",
		"https://evildiscord.com/api/webhooks/1/x",
		"https://notdiscord.com/api/webhooks/1/x",
	}
	for _, url := range cases {
		if _, err := notify.NewDiscord(url); err == nil {
			t.Errorf("expected %q to be rejected", url)
		}
	}
}

func TestSlack_AcceptsValidWebhookURL(t *testing.T) {
	for _, url := range []string{
		"https://slack.com/services/T000/B000/abc123",
		"https://hooks.slack.com/services/T000/B000/abc123",
	} {
		if _, err := notify.NewSlack(url); err != nil {
			t.Errorf("a valid slack webhook was rejected: %v (%s)", err, url)
		}
	}
}

// A webhook that redirects must not be followed: the new host was never
// validated, and following it is exactly how an allowed host becomes SSRF.
func TestNotify_DoesNotFollowRedirects(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target must never be reached")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(internal.Close)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	if err := notify.SlackForTest(redirector.URL).Notify(context.Background(), sampleReport()); err == nil {
		t.Error("expected a redirect response to be reported as a delivery failure")
	}
}

func TestDiscord_AcceptsValidWebhookURLs(t *testing.T) {
	for _, url := range []string{
		"https://discord.com/api/webhooks/123/abc",
		"https://discordapp.com/api/webhooks/123/abc",
	} {
		if _, err := notify.NewDiscord(url); err != nil {
			t.Errorf("a valid discord webhook was rejected: %v (%s)", err, url)
		}
	}
}

func TestParseProvider(t *testing.T) {
	for raw, want := range map[string]notify.Provider{
		"slack": notify.ProviderSlack, "Discord": notify.ProviderDiscord,
		"none": notify.ProviderNone, "": notify.ProviderNone,
	} {
		got, err := notify.ParseProvider(raw)
		if err != nil || got != want {
			t.Errorf("ParseProvider(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	if _, err := notify.ParseProvider("teams"); err == nil {
		t.Error("expected an unsupported provider to be rejected")
	}
}

// A workspace with no destination must follow the same path, not a nil notifier.
func TestNew_NoneYieldsWorkingDiscard(t *testing.T) {
	n, err := notify.New(notify.ProviderNone, "")
	if err != nil {
		t.Fatalf("building discard notifier: %v", err)
	}
	if err := n.Notify(context.Background(), sampleReport()); err != nil {
		t.Errorf("discard notifier should always succeed: %v", err)
	}
}

// The payload must carry the verdict, the commands and the read-only disclaimer,
// because that is what an on-call engineer acts on.
func TestSlackPayload_CarriesVerdictAndCommands(t *testing.T) {
	srv, captured := captureServer(t, http.StatusOK)

	s := notify.SlackForTest(srv.URL)
	if err := s.Notify(context.Background(), sampleReport()); err != nil {
		t.Fatalf("notify failed: %v", err)
	}

	raw, _ := json.Marshal(*captured)
	body := string(raw)

	for _, want := range []string{
		"PostgreSQL connections exhausted",
		"Code-level",
		"application",
		"pg_stat_activity",
		"read-only",
		"no hypothesis",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("slack payload is missing %q", want)
		}
	}
	if !strings.Contains(body, "attachments") {
		t.Error("expected a coloured attachment")
	}
}

func TestDiscordPayload_CarriesVerdictAndCommands(t *testing.T) {
	srv, captured := captureServer(t, http.StatusOK)

	d := notify.DiscordForTest(srv.URL)
	if err := d.Notify(context.Background(), sampleReport()); err != nil {
		t.Fatalf("notify failed: %v", err)
	}

	raw, _ := json.Marshal(*captured)
	body := string(raw)

	for _, want := range []string{"embeds", "Code-level", "pg_stat_activity", "read-only"} {
		if !strings.Contains(body, want) {
			t.Errorf("discord payload is missing %q", want)
		}
	}
}

// A rejected delivery must surface, not be swallowed.
func TestNotify_SurfacesRejection(t *testing.T) {
	srv, _ := captureServer(t, http.StatusForbidden)

	if err := notify.SlackForTest(srv.URL).Notify(context.Background(), sampleReport()); err == nil {
		t.Error("expected a 403 to be reported as an error")
	}
}
