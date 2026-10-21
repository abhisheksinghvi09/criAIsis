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

	// PostPayload sends a full chat.postMessage body (blocks, attachments) and
	// returns the message timestamp, which is the thread key for replies.
	PostPayload(ctx context.Context, token string, payload map[string]any) (ts string, err error)
	// CreatePrivateChannel creates a private channel and returns its id.
	CreatePrivateChannel(ctx context.Context, token, name string) (id string, err error)
	// InviteUser adds one user to a channel.
	InviteUser(ctx context.Context, token, channel, userID string) error
	// LookupUserByEmail resolves a Slack user id from an email address.
	LookupUserByEmail(ctx context.Context, token, email string) (id string, err error)
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
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
	_, err := c.call(ctx, token, endpoint, "application/json; charset=utf-8", payload)
	return err
}

// call performs one Slack Web API request and returns the decoded response body.
// payload is JSON-encoded, or sent as a form when it is url.Values.
func (c *DefaultClient) call(ctx context.Context, token, endpoint, contentType string, payload any) (map[string]any, error) {
	var body io.Reader
	if form, ok := payload.(url.Values); ok {
		contentType = "application/x-www-form-urlencoded"
		body = strings.NewReader(form.Encode())
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshaling json: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling slack api %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slack api returned status %d", resp.StatusCode)
	}

	// Slack reports most failures (not_in_channel, invalid_auth, ...) as 200 with ok:false.
	var out map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return nil, fmt.Errorf("parsing slack api %s response: %w", endpoint, err)
	}
	if ok, _ := out["ok"].(bool); !ok {
		msg, _ := out["error"].(string)
		return nil, fmt.Errorf("slack api %s: %s", endpoint, msg)
	}
	return out, nil
}

// maxResponseBytes bounds any Slack response we read.
const maxResponseBytes = 1 << 20

func (c *DefaultClient) PostPayload(ctx context.Context, token string, payload map[string]any) (string, error) {
	out, err := c.call(ctx, token, "/chat.postMessage", "application/json; charset=utf-8", payload)
	if err != nil {
		return "", err
	}
	ts, _ := out["ts"].(string)
	return ts, nil
}

func (c *DefaultClient) CreatePrivateChannel(ctx context.Context, token, name string) (string, error) {
	out, err := c.call(ctx, token, "/conversations.create", "application/json; charset=utf-8",
		map[string]any{"name": name, "is_private": true})
	if err != nil {
		return "", err
	}
	ch, _ := out["channel"].(map[string]any)
	id, _ := ch["id"].(string)
	if id == "" {
		return "", fmt.Errorf("slack api /conversations.create: no channel id in response")
	}
	return id, nil
}

func (c *DefaultClient) InviteUser(ctx context.Context, token, channel, userID string) error {
	_, err := c.call(ctx, token, "/conversations.invite", "application/json; charset=utf-8",
		map[string]any{"channel": channel, "users": userID})
	return err
}

func (c *DefaultClient) LookupUserByEmail(ctx context.Context, token, email string) (string, error) {
	out, err := c.call(ctx, token, "/users.lookupByEmail", "", url.Values{"email": {email}})
	if err != nil {
		return "", err
	}
	user, _ := out["user"].(map[string]any)
	id, _ := user["id"].(string)
	if id == "" {
		return "", fmt.Errorf("slack api /users.lookupByEmail: no user id in response")
	}
	return id, nil
}
