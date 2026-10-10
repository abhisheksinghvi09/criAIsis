package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/job"
	"criaisis/internal/service/incident"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// maxAlertBody caps an inbound webhook. Provider payloads are kilobytes; anything
// far larger is a mistake or an attempt to exhaust memory at an unauthenticated edge.
const maxAlertBody = 1 << 20

// webhookTokenHeader carries the per-workspace ingestion credential.
const webhookTokenHeader = "X-Criaisis-Token"

// snsConfirmTimeout bounds the outbound subscription handshake.
const snsConfirmTimeout = 10 * time.Second

// AlertHandler ingests monitoring webhooks and opens incidents from them.
type AlertHandler struct {
	workspaces repository.WorkspaceRepository
	incidents  *incident.Service
	log        *zerolog.Logger
}

// NewAlertHandler wires the webhook ingress.
func NewAlertHandler(workspaces repository.WorkspaceRepository, incidents *incident.Service, log *zerolog.Logger) *AlertHandler {
	return &AlertHandler{workspaces: workspaces, incidents: incidents, log: log}
}

// Receive handles POST /api/v1/integrations/alerts/{provider}.
//
// It authenticates the workspace, parses the provider payload, persists the
// incident and queues the clash, then returns. The debate runs on a worker so this
// path stays well inside the provider's delivery timeout.
func (h *AlertHandler) Receive(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")

	ws, err := h.authenticate(r)
	if err != nil {
		// Do not distinguish unknown workspace from bad token: that difference is an
		// oracle for probing which tenants exist.
		h.log.Warn().Err(err).Str("provider", provider).Msg("rejected alert webhook")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid workspace or token"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxAlertBody))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read request body"})
		return
	}

	// SNS refuses to deliver any alarm until the subscription is confirmed, so the
	// handshake is completed here rather than rejected as an unparseable alert.
	if confirmation, ok := DetectSNSConfirmation(body); ok {
		if err := h.confirmSNS(r.Context(), confirmation); err != nil {
			h.log.Error().Err(err).Msg("could not confirm sns subscription")
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not confirm subscription"})
			return
		}
		h.log.Info().Str("topic_arn", confirmation.TopicArn).Msg("sns subscription confirmed")
		writeJSON(w, http.StatusOK, map[string]string{"status": "subscription confirmed"})
		return
	}

	alert, err := ParseAlert(provider, body)
	if err != nil {
		h.log.Warn().Err(err).Str("provider", provider).Msg("could not parse alert payload")
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	channelID, err := value.NewSlackChannelID(firstNonEmpty(r.URL.Query().Get("channel"), "C_UNROUTED"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid channel id"})
		return
	}
	// Webhook incidents have no Slack thread yet; the posting step creates one, and
	// this synthetic timestamp keeps the (workspace, channel, thread) key unique.
	threadTS, err := value.NewSlackThreadTS(fmt.Sprintf("%d.000000", time.Now().UTC().UnixNano()))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not allocate thread id"})
		return
	}
	createdBy, err := value.NewSlackUserID("U_WEBHOOK")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not identify webhook author"})
		return
	}

	alertContext := alert.Context
	inc, err := h.incidents.Open(r.Context(), incident.NewIncidentInput{
		WorkspaceID: ws.ID(),
		Title:       alert.Title,
		Description: alert.Description,
		Severity:    alert.Severity,
		Trigger:     value.TriggerTypeWebhook,
		ChannelID:   channelID,
		ThreadTS:    threadTS,
		CreatedBy:   createdBy,
		Context:     &alertContext,
	})

	switch {
	case errors.Is(err, job.ErrQueueFull):
		// The incident is durable but the debate was shed. 503 tells the provider to
		// retry, which is the honest answer.
		h.log.Error().Str("incident_id", inc.ID().String()).Msg("queue saturated, clash not started")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"incident_id": inc.ID().String(),
			"error":       "investigation queue is saturated, retry shortly",
		})
		return
	case err != nil:
		h.log.Error().Err(err).Str("provider", provider).Msg("could not open incident from alert")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not open incident"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"incident_id": inc.ID().String(),
		"severity":    inc.Severity().String(),
		"status":      "investigating",
	})
}

// authenticate resolves the workspace from the query string and verifies its token.
func (h *AlertHandler) authenticate(r *http.Request) (*entity.Workspace, error) {
	teamID := r.URL.Query().Get("workspace")
	if teamID == "" {
		return nil, errors.New("workspace parameter is required")
	}

	ws, err := h.workspaces.GetBySlackTeamID(r.Context(), teamID)
	if err != nil {
		return nil, fmt.Errorf("resolving workspace: %w", err)
	}
	if !ws.VerifyWebhookSecret(r.Header.Get(webhookTokenHeader)) {
		return nil, errors.New("webhook token does not match")
	}
	return ws, nil
}

// confirmSNS completes the handshake by fetching the URL Amazon supplied.
// Only https URLs under amazonaws.com are followed, so a forged payload cannot
// turn this endpoint into a request forgery primitive.
func (h *AlertHandler) confirmSNS(ctx context.Context, confirmation *SNSConfirmation) error {
	parsed, err := url.Parse(confirmation.SubscribeURL)
	if err != nil {
		return fmt.Errorf("unparseable SubscribeURL: %w", err)
	}
	if parsed.Scheme != "https" || !strings.HasSuffix(parsed.Hostname(), ".amazonaws.com") {
		return fmt.Errorf("refusing to fetch SubscribeURL outside amazonaws.com: %s", parsed.Host)
	}

	ctx, cancel := context.WithTimeout(ctx, snsConfirmTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, confirmation.SubscribeURL, nil)
	if err != nil {
		return fmt.Errorf("building confirmation request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching SubscribeURL: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 300 {
		return fmt.Errorf("SubscribeURL returned status %d", resp.StatusCode)
	}
	return nil
}

// writeJSON emits a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
