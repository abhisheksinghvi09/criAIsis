// Package oncall answers one question: who is on call right now? It reads the
// tenant's own PagerDuty or Opsgenie schedule with the tenant's own API key.
package oncall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider names a supported on-call source.
type Provider string

const (
	ProviderNone      Provider = "none"
	ProviderPagerDuty Provider = "pagerduty"
	ProviderOpsgenie  Provider = "opsgenie"
)

// ParseProvider validates a configured on-call source.
func ParseProvider(raw string) (Provider, error) {
	switch p := Provider(strings.ToLower(strings.TrimSpace(raw))); p {
	case ProviderNone, "":
		return ProviderNone, nil
	case ProviderPagerDuty, ProviderOpsgenie:
		return p, nil
	default:
		return "", fmt.Errorf("unsupported on-call provider %q: expected none, pagerduty or opsgenie", raw)
	}
}

func (p Provider) String() string { return string(p) }

// Lister returns the email addresses of whoever is on call right now.
type Lister interface {
	OnCallEmails(ctx context.Context) ([]string, error)
}

const (
	requestTimeout = 10 * time.Second
	maxBody        = 1 << 20
)

// New builds the lister for a provider. scheduleID is required: asking a vendor
// for everyone on call across the account would page the wrong people.
func New(provider Provider, apiKey, scheduleID string) (Lister, error) {
	if strings.TrimSpace(scheduleID) == "" {
		return nil, fmt.Errorf("%s needs a schedule id", provider)
	}
	switch provider {
	case ProviderPagerDuty:
		return &pagerDuty{c: newClient(), base: "https://api.pagerduty.com", key: apiKey, schedule: scheduleID}, nil
	case ProviderOpsgenie:
		return &opsgenie{c: newClient(), base: "https://api.opsgenie.com", key: apiKey, schedule: scheduleID}, nil
	default:
		return nil, fmt.Errorf("unsupported on-call provider %q", provider)
	}
}

func newClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		// Vendor hosts are fixed; a redirect would carry the API key elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// getJSON performs an authenticated GET and decodes a bounded JSON response.
func getJSON(ctx context.Context, c *http.Client, endpoint string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("on-call request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("on-call provider returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out)
}

type pagerDuty struct {
	c                   *http.Client
	base, key, schedule string
}

func (p *pagerDuty) OnCallEmails(ctx context.Context) ([]string, error) {
	q := url.Values{"schedule_ids[]": {p.schedule}, "include[]": {"users"}, "earliest": {"true"}}
	var out struct {
		OnCalls []struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"oncalls"`
	}
	err := getJSON(ctx, p.c, p.base+"/oncalls?"+q.Encode(), map[string]string{
		"Authorization": "Token token=" + p.key,
		"Accept":        "application/vnd.pagerduty+json;version=2",
	}, &out)
	if err != nil {
		return nil, err
	}
	var emails []string
	for _, oc := range out.OnCalls {
		emails = append(emails, oc.User.Email)
	}
	return dedupe(emails), nil
}

type opsgenie struct {
	c                   *http.Client
	base, key, schedule string
}

func (o *opsgenie) OnCallEmails(ctx context.Context) ([]string, error) {
	endpoint := o.base + "/v2/schedules/" + url.PathEscape(o.schedule) + "/on-calls?scheduleIdentifierType=id&flat=true"
	var out struct {
		Data struct {
			OnCallRecipients []string `json:"onCallRecipients"`
		} `json:"data"`
	}
	if err := getJSON(ctx, o.c, endpoint, map[string]string{"Authorization": "GenieKey " + o.key}, &out); err != nil {
		return nil, err
	}
	return dedupe(out.Data.OnCallRecipients), nil
}

// dedupe drops blanks and repeats (a person can hold several escalation levels).
func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}
