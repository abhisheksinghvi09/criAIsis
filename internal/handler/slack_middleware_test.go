package handler_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"criaisis/internal/handler"

	"github.com/rs/zerolog"
)

func TestSlackSignatureMiddleware(t *testing.T) {
	secret := "test-signing-secret"
	logger := zerolog.Nop()
	mw := handler.NewSlackSignatureMiddleware(secret, &logger)

	successHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	t.Run("valid signature and fresh timestamp", func(t *testing.T) {
		body := "command=%2Fcriaisis&text=investigate"
		ts := fmt.Sprintf("%d", time.Now().UTC().Unix())
		baseString := fmt.Sprintf("v0:%s:%s", ts, body)

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(baseString))
		sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(body))
		req.Header.Set("X-Slack-Request-Timestamp", ts)
		req.Header.Set("X-Slack-Signature", sig)
		rec := httptest.NewRecorder()

		mw.Handler(successHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing headers returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/slack/command", strings.NewReader("test"))
		rec := httptest.NewRecorder()

		mw.Handler(successHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("expired timestamp returns 400", func(t *testing.T) {
		body := "test"
		oldTS := fmt.Sprintf("%d", time.Now().UTC().Add(-400*time.Second).Unix())
		baseString := fmt.Sprintf("v0:%s:%s", oldTS, body)

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(baseString))
		sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest("POST", "/slack/command", strings.NewReader(body))
		req.Header.Set("X-Slack-Request-Timestamp", oldTS)
		req.Header.Set("X-Slack-Signature", sig)
		rec := httptest.NewRecorder()

		mw.Handler(successHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("invalid signature returns 400", func(t *testing.T) {
		ts := fmt.Sprintf("%d", time.Now().UTC().Unix())
		req := httptest.NewRequest("POST", "/slack/command", strings.NewReader("test"))
		req.Header.Set("X-Slack-Request-Timestamp", ts)
		req.Header.Set("X-Slack-Signature", "v0=badbadbad")
		rec := httptest.NewRecorder()

		mw.Handler(successHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})
}

func TestWorkspaceRateLimitMiddleware(t *testing.T) {
	logger := zerolog.Nop()
	limit := 3
	mw := handler.NewWorkspaceRateLimitMiddleware(limit, 1*time.Minute, &logger)

	successHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Make 3 allowed requests
	for i := 0; i < limit; i++ {
		req := httptest.NewRequest("POST", "/slack/command?team_id=T123", strings.NewReader(""))
		rec := httptest.NewRecorder()
		mw.Handler(successHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// 4th request must be rate limited (429)
	req := httptest.NewRequest("POST", "/slack/command?team_id=T123", strings.NewReader(""))
	rec := httptest.NewRecorder()
	mw.Handler(successHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header to be set")
	}
}
