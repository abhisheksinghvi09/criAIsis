package slack_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"criaisis/internal/infrastructure/slack"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *slack.DefaultClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := slack.NewClient(nil)
	c.SetBaseURL(srv.URL)
	return c
}

func TestExchangeOAuthCode_Success(t *testing.T) {
	var gotForm string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth.v2.access" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		gotForm = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxb-abc","token_type":"bot","team":{"id":"T123","name":"Acme"}}`))
	})

	res, err := c.ExchangeOAuthCode(context.Background(), "client-id", "client-secret", "the-code")
	if err != nil {
		t.Fatalf("ExchangeOAuthCode: %v", err)
	}
	if !res.OK || res.AccessToken != "xoxb-abc" || res.Team.ID != "T123" || res.Team.Name != "Acme" {
		t.Errorf("unexpected response: %+v", res)
	}
	for _, want := range []string{"client_id=client-id", "client_secret=client-secret", "code=the-code"} {
		if !strings.Contains(gotForm, want) {
			t.Errorf("request form %q missing %q", gotForm, want)
		}
	}
}

func TestExchangeOAuthCode_SlackRejection(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_code"}`))
	})

	res, err := c.ExchangeOAuthCode(context.Background(), "id", "secret", "bad-code")
	if err == nil {
		t.Fatal("expected an error for a rejected oauth exchange")
	}
	if res == nil || res.Error != "invalid_code" {
		t.Errorf("expected the error response to still be returned, got %+v", res)
	}
}

func TestExchangeOAuthCode_MalformedResponseBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})

	if _, err := c.ExchangeOAuthCode(context.Background(), "id", "secret", "code"); err == nil {
		t.Error("expected malformed json to produce an error")
	}
}

func TestExchangeOAuthCode_TransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // closed before use, so any request fails at the transport

	c := slack.NewClient(nil)
	c.SetBaseURL(srv.URL)

	if _, err := c.ExchangeOAuthCode(context.Background(), "id", "secret", "code"); err == nil {
		t.Error("expected a transport error against a closed server")
	}
}

func TestPostMessage_SendsChannelTextAndThread(t *testing.T) {
	var captured map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-token" {
			t.Errorf("expected bearer token header, got %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
	})

	if err := c.PostMessage(context.Background(), "xoxb-token", "C123", "1700.0001", "the verdict"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if captured["channel"] != "C123" || captured["text"] != "the verdict" || captured["thread_ts"] != "1700.0001" {
		t.Errorf("unexpected payload: %+v", captured)
	}
}

func TestPostMessage_OmitsThreadTSWhenEmpty(t *testing.T) {
	var captured map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
	})

	if err := c.PostMessage(context.Background(), "xoxb-token", "C123", "", "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, present := captured["thread_ts"]; present {
		t.Errorf("expected no thread_ts key, got %+v", captured)
	}
}

func TestPostMessage_NonOKStatusIsAnError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	if err := c.PostMessage(context.Background(), "xoxb-token", "C123", "", "hello"); err == nil {
		t.Error("expected a non-200 response to be reported as an error")
	}
}

func TestPostEphemeral_SendsChannelUserAndText(t *testing.T) {
	var captured map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postEphemeral" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
	})

	if err := c.PostEphemeral(context.Background(), "xoxb-token", "C123", "U999", "only you can see this"); err != nil {
		t.Fatalf("PostEphemeral: %v", err)
	}
	if captured["channel"] != "C123" || captured["user"] != "U999" || captured["text"] != "only you can see this" {
		t.Errorf("unexpected payload: %+v", captured)
	}
}
