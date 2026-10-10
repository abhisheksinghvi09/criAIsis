package slack

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

// OAuthResponse models the payload returned by Slack's oauth.v2.access endpoint.
type OAuthResponse struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Team        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
}

// Client abstracts outbound Slack Web API interactions.
type Client interface {
	ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code string) (*OAuthResponse, error)
	PostMessage(ctx context.Context, token, channel, threadTS, text string) error
	PostEphemeral(ctx context.Context, token, channel, userID, text string) error
}

// DefaultClient implements Client using standard net/http against Slack's Web API.
type DefaultClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient initializes a default Slack API client.
func NewClient(httpClient *http.Client) *DefaultClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &DefaultClient{
		httpClient: httpClient,
		baseURL:    "https://slack.com/api",
	}
}

// SetBaseURL allows overriding the endpoint in testing.
func (c *DefaultClient) SetBaseURL(url string) {
	c.baseURL = strings.TrimRight(url, "/")
}

func (c *DefaultClient) ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code string) (*OAuthResponse, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/oauth.v2.access", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating oauth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing oauth request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading oauth response: %w", err)
	}

	var res OAuthResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("parsing oauth response: %w", err)
	}

	if !res.OK {
		return &res, fmt.Errorf("slack oauth error: %s", res.Error)
	}
	return &res, nil
}

func (c *DefaultClient) PostMessage(ctx context.Context, token, channel, threadTS, text string) error {
	payload := map[string]any{
		"channel": channel,
		"text":    text,
	}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	return c.postJSON(ctx, token, "/chat.postMessage", payload)
}

func (c *DefaultClient) PostEphemeral(ctx context.Context, token, channel, userID, text string) error {
	payload := map[string]any{
		"channel": channel,
		"user":    userID,
		"text":    text,
	}
	return c.postJSON(ctx, token, "/chat.postEphemeral", payload)
}

func (c *DefaultClient) postJSON(ctx context.Context, token, endpoint string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling json: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling slack api %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack api returned status %d", resp.StatusCode)
	}
	return nil
}
