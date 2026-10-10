package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// WorkspaceRateLimitMiddleware limits inbound Slack requests per workspace to 30 req/min (NFR2).
type WorkspaceRateLimitMiddleware struct {
	mu      sync.Mutex
	windows map[string][]time.Time
	limit   int
	window  time.Duration
	log     *zerolog.Logger
}

// NewWorkspaceRateLimitMiddleware creates a sliding-window rate limiter.
func NewWorkspaceRateLimitMiddleware(limit int, window time.Duration, log *zerolog.Logger) *WorkspaceRateLimitMiddleware {
	if limit <= 0 {
		limit = 30
	}
	if window <= 0 {
		window = 1 * time.Minute
	}
	return &WorkspaceRateLimitMiddleware{
		windows: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
		log:     log,
	}
}

// Handler returns the HTTP middleware function.
func (m *WorkspaceRateLimitMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read request body"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Attempt to extract team_id from form body or query
		teamID := r.URL.Query().Get("team_id")
		if teamID == "" {
			formVals, _ := url.ParseQuery(string(bodyBytes))
			teamID = formVals.Get("team_id")
		}
		if teamID == "" {
			teamID = "default"
		}

		m.mu.Lock()
		now := time.Now().UTC()
		cutoff := now.Add(-m.window)

		// Prune timestamps older than window
		var valid []time.Time
		for _, ts := range m.windows[teamID] {
			if ts.After(cutoff) {
				valid = append(valid, ts)
			}
		}

		if len(valid) >= m.limit {
			// Rate limit exceeded
			oldest := valid[0]
			retryAfter := int(m.window.Seconds() - now.Sub(oldest).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			m.windows[teamID] = valid
			m.mu.Unlock()

			m.log.Info().
				Str("team_id", teamID).
				Int("count", len(valid)).
				Int("retry_after", retryAfter).
				Msg("slack request rate limit exceeded (HTTP 429)")

			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error":       "rate limit exceeded (30 requests/minute)",
				"retry_after": fmt.Sprintf("%ds", retryAfter),
			})
			return
		}

		valid = append(valid, now)
		m.windows[teamID] = valid
		m.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}
