package handler

import (
	"bytes"
	"encoding/json"
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

// allow records one request for key and reports whether it fits the window. When
// it does not, retryAfter is the number of seconds until the oldest request ages out.
func (m *WorkspaceRateLimitMiddleware) allow(key string) (ok bool, retryAfter int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	cutoff := now.Add(-m.window)

	// Sweep idle keys occasionally so the map cannot grow without bound.
	if len(m.windows) > 1024 {
		for k, stamps := range m.windows {
			if len(stamps) == 0 || !stamps[len(stamps)-1].After(cutoff) {
				delete(m.windows, k)
			}
		}
	}

	var valid []time.Time
	for _, ts := range m.windows[key] {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	if len(valid) >= m.limit {
		retryAfter = int(m.window.Seconds() - now.Sub(valid[0]).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		m.windows[key] = valid
		return false, retryAfter
	}
	m.windows[key] = append(valid, now)
	return true, 0
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

		teamID := r.URL.Query().Get("team_id")
		if teamID == "" {
			teamID = slackTeamID(bodyBytes)
		}
		if teamID == "" {
			teamID = "default"
		}

		if ok, retryAfter := m.allow(teamID); !ok {
			m.log.Info().
				Str("team_id", teamID).
				Int("retry_after", retryAfter).
				Msg("slack request rate limit exceeded (HTTP 429)")

			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error":       fmt.Sprintf("rate limit exceeded (%d requests per %s)", m.limit, m.window),
				"retry_after": fmt.Sprintf("%ds", retryAfter),
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// slackTeamID finds the tenant in the three body shapes Slack sends: a form with
// team_id (slash commands), a form whose "payload" field is JSON with team.id
// (interactivity), and a JSON body with team_id (Events API). Without this,
// interactivity and events all share one bucket across every tenant.
func slackTeamID(body []byte) string {
	if form, err := url.ParseQuery(string(body)); err == nil {
		if id := form.Get("team_id"); id != "" {
			return id
		}
		if payload := form.Get("payload"); payload != "" {
			body = []byte(payload)
		}
	}
	var msg struct {
		TeamID string `json:"team_id"`
		Team   struct {
			ID string `json:"id"`
		} `json:"team"`
	}
	if json.Unmarshal(body, &msg) != nil {
		return ""
	}
	if msg.TeamID != "" {
		return msg.TeamID
	}
	return msg.Team.ID
}
