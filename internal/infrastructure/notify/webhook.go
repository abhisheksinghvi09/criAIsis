package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// postTimeout bounds a delivery attempt. The debate is already done by this point,
// so a slow channel must not hold a worker open indefinitely.
const postTimeout = 15 * time.Second

// severityColor maps a tier onto the accent stripe both providers support.
func severityColor(severity string) (hex string, decimal int) {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "sev-1":
		return "#E5484D", 0xE5484D
	case "sev-2":
		return "#F5A524", 0xF5A524
	case "sev-3":
		return "#38BDF8", 0x38BDF8
	default:
		return "#5F6C7B", 0x5F6C7B
	}
}

// validateWebhookURL rejects anything that is not an https webhook, so a
// misconfiguration cannot silently send incident detail over plaintext.
func validateWebhookURL(raw, wantHost string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("webhook url is empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webhook url is not a valid url: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("webhook url must use https, got %q", parsed.Scheme)
	}
	// Exact host or a true subdomain only: HasSuffix alone would also match
	// "evilslack.com", which ends in "slack.com" but is not Slack.
	host := parsed.Hostname()
	if wantHost != "" && host != wantHost && !strings.HasSuffix(host, "."+wantHost) {
		return fmt.Errorf("webhook url host %q is not %s", host, wantHost)
	}
	return nil
}

// noRedirectClient refuses to follow redirects, so a webhook whose host passes
// validation cannot 302 the request to an internal address afterward.
func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// post delivers a JSON payload and interprets the response.
func post(ctx context.Context, client *http.Client, webhookURL string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding notification: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, postTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// The URL is a credential, so it must not reach the logs via the error.
		return fmt.Errorf("delivering notification: %w", redactURL(err, webhookURL))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("notification rejected with status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}

// redactURL strips a webhook credential out of an error message.
func redactURL(err error, webhookURL string) error {
	if webhookURL == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), webhookURL, "[redacted webhook url]"))
}
