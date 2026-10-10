package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"
)

// SlackSignatureMiddleware verifies inbound requests from Slack using HMAC-SHA256 signature
// and provides replay protection by enforcing a 5-minute timestamp window.
type SlackSignatureMiddleware struct {
	signingSecret string
	log           *zerolog.Logger
}

// NewSlackSignatureMiddleware initializes the signature verification middleware.
func NewSlackSignatureMiddleware(signingSecret string, log *zerolog.Logger) *SlackSignatureMiddleware {
	return &SlackSignatureMiddleware{
		signingSecret: signingSecret,
		log:           log,
	}
}

// Handler returns the HTTP middleware function.
func (m *SlackSignatureMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tsHeader := r.Header.Get("X-Slack-Request-Timestamp")
		sigHeader := r.Header.Get("X-Slack-Signature")

		if tsHeader == "" || sigHeader == "" {
			m.log.Warn().Msg("slack request missing required verification headers")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing slack verification headers"})
			return
		}

		tsInt, err := strconv.ParseInt(tsHeader, 10, 64)
		if err != nil {
			m.log.Warn().Str("timestamp", tsHeader).Msg("invalid slack timestamp header format")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid timestamp header"})
			return
		}

		nowUnix := time.Now().UTC().Unix()
		diff := math.Abs(float64(nowUnix - tsInt))
		if diff > 300 {
			m.log.Warn().Float64("delta_seconds", diff).Msg("slack request timestamp outside 5-minute window (replay rejected)")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request timestamp expired"})
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			m.log.Error().Err(err).Msg("failed reading slack request body")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read request body"})
			return
		}
		// Reset body for downstream handlers
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		baseString := fmt.Sprintf("v0:%s:%s", tsHeader, string(bodyBytes))
		mac := hmac.New(sha256.New, []byte(m.signingSecret))
		mac.Write([]byte(baseString))
		expectedSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

		if subtle.ConstantTimeCompare([]byte(sigHeader), []byte(expectedSig)) != 1 {
			m.log.Warn().Str("signature", sigHeader).Msg("invalid slack signature")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request signature"})
			return
		}

		next.ServeHTTP(w, r)
	})
}
