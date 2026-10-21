package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/llm"
	"criaisis/internal/infrastructure/notify"
	"criaisis/internal/infrastructure/oncall"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/tenant"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// maxBodyBytes caps a management request body.
const maxBodyBytes = 1 << 18

// WorkspaceHandler is the self-service surface where a customer supplies their own
// model credentials and chooses where investigations are posted.
type WorkspaceHandler struct {
	workspaces repository.WorkspaceRepository
	personas   repository.PersonaRepository
	settings   repository.SettingsRepository
	tx         repository.TransactionManager
	resolver   *tenant.Resolver
	incidents  *incident.Service
	cipher     *crypto.Cipher
	prov       *Provisioner
	sandbox    bool
	log        *zerolog.Logger
}

// NewWorkspaceHandler wires the management API.
func NewWorkspaceHandler(
	workspaces repository.WorkspaceRepository,
	personas repository.PersonaRepository,
	settings repository.SettingsRepository,
	tx repository.TransactionManager,
	resolver *tenant.Resolver,
	incidents *incident.Service,
	cipher *crypto.Cipher,
	defaults entity.SettingsDefaults,
	log *zerolog.Logger,
) *WorkspaceHandler {
	return &WorkspaceHandler{
		workspaces: workspaces, personas: personas, settings: settings, tx: tx, resolver: resolver,
		incidents: incidents, cipher: cipher, log: log,
		prov: NewProvisioner(workspaces, settings, personas, tx, defaults),
	}
}

// WithSandbox records whether incident reproduction is enabled, so the dashboard
// can hide a feature the server does not serve.
func (h *WorkspaceHandler) WithSandbox(enabled bool) *WorkspaceHandler {
	h.sandbox = enabled
	return h
}

// workspaceParam reads the tenant identifier from the route.
func workspaceParam(r *http.Request) string { return chi.URLParam(r, "workspace") }

type createWorkspaceRequest struct {
	TeamID string `json:"team_id"`
	Name   string `json:"name"`
}

// Create provisions a tenant. Requires the platform admin credential.
//
// It returns the workspace's two credentials once. Only their digests are stored,
// so neither can be recovered later; both can be rotated.
func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWorkspaceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.TeamID == "" || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team_id and name are required"})
		return
	}

	// Until a Slack app exists there is no bot token; a sealed placeholder keeps
	// the column honest rather than pretending one was installed.
	placeholder, err := h.cipher.Seal("pending-slack-install", crypto.BotTokenAAD(req.TeamID))
	if err != nil {
		h.fail(w, "sealing placeholder token", err)
		return
	}

	ws, err := entity.NewWorkspace(req.TeamID, req.Name, placeholder)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	creds, err := h.prov.Provision(r.Context(), ws)
	if err != nil {
		h.fail(w, "provisioning workspace", err)
		return
	}
	adminKey, alertToken := creds.AdminKey, creds.AlertToken

	writeJSON(w, http.StatusCreated, map[string]any{
		"team_id":     ws.SlackTeamID(),
		"name":        ws.SlackTeamName(),
		"admin_key":   adminKey,
		"alert_token": alertToken,
		"note":        "Both credentials are shown once and stored only as digests. Save them now.",
		"next": []string{
			"PUT /api/v1/workspaces/" + ws.SlackTeamID() + "/llm with your Anthropic key",
			"PUT /api/v1/workspaces/" + ws.SlackTeamID() + "/embeddings with your embeddings key",
			"PUT /api/v1/workspaces/" + ws.SlackTeamID() + "/notifications with your Slack or Discord webhook",
		},
	})
}

// Show reports a tenant's configuration. Secrets are never returned: only whether
// each is set, so an operator can see what remains without the value leaking.
func (h *WorkspaceHandler) Show(w http.ResponseWriter, r *http.Request) {
	ws, settings, ok := h.authenticated(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"team_id":  ws.SlackTeamID(),
		"name":     ws.SlackTeamName(),
		"features": map[string]bool{"sandbox": h.sandbox},
		"oncall": map[string]any{
			"provider":    settings.OnCallProvider(),
			"schedule_id": settings.OnCallScheduleID(),
			"api_key_set": len(settings.OnCallAPIKeyEncrypted()) > 0,
		},
		"llm": map[string]any{
			"provider":         settings.LLMProvider(),
			"api_key_set":      settings.HasLLMKey(),
			"specialist_model": settings.SpecialistModel(),
			"synthesis_model":  settings.SynthesisModel(),
		},
		"embeddings": map[string]any{
			"provider":    settings.EmbeddingProvider(),
			"api_key_set": settings.HasEmbeddingKey(),
			"model":       settings.EmbeddingModel(),
			"base_url":    settings.EmbeddingBaseURL(),
		},
		"notifications": map[string]any{
			"provider":    settings.NotifyProvider(),
			"webhook_set": len(settings.NotifyWebhookEncrypted()) > 0,
		},
		"ready_to_investigate": settings.IsReadyToInvestigate(),
		"missing":              settings.MissingRequirements(),
	})
}

type llmRequest struct {
	APIKey          string `json:"api_key"`
	SpecialistModel string `json:"specialist_model"`
	SynthesisModel  string `json:"synthesis_model"`
}

// SetLLM stores the tenant's own model credential, after proving it works.
func (h *WorkspaceHandler) SetLLM(w http.ResponseWriter, r *http.Request) {
	ws, settings, ok := h.authenticated(w, r)
	if !ok {
		return
	}

	var req llmRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_key is required"})
		return
	}

	specialist := firstNonEmpty(req.SpecialistModel, settings.SpecialistModel())
	synthesis := firstNonEmpty(req.SynthesisModel, settings.SynthesisModel())

	// Verify before storing, so a bad key is rejected at setup rather than
	// discovered in the middle of an incident.
	if err := llm.VerifyAnthropic(r.Context(), llm.AnthropicOptions{
		APIKey: req.APIKey, SpecialistModel: specialist, SynthesisModel: synthesis,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the model credential was rejected: " + err.Error()})
		return
	}

	sealed, err := h.cipher.Seal(req.APIKey, crypto.SettingAAD(ws.ID().String(), crypto.FieldLLMKey))
	if err != nil {
		h.fail(w, "sealing model key", err)
		return
	}
	h.updateSettings(w, r, ws, "model credential verified and stored", func(s *entity.WorkspaceSettings) error {
		return s.SetLLM("anthropic", sealed, specialist, synthesis)
	})
}

type embeddingRequest struct {
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	BaseURL string `json:"base_url"`
}

// SetEmbeddings stores the tenant's retrieval credential, after proving it works
// and returns the dimension the schema requires.
func (h *WorkspaceHandler) SetEmbeddings(w http.ResponseWriter, r *http.Request) {
	ws, settings, ok := h.authenticated(w, r)
	if !ok {
		return
	}

	var req embeddingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_key is required"})
		return
	}

	opts := llm.EmbeddingOptions{
		APIKey:  req.APIKey,
		Model:   firstNonEmpty(req.Model, settings.EmbeddingModel()),
		BaseURL: firstNonEmpty(req.BaseURL, settings.EmbeddingBaseURL()),
	}
	if err := llm.VerifyEmbeddings(r.Context(), opts); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the embedding credential was rejected: " + err.Error()})
		return
	}

	sealed, err := h.cipher.Seal(req.APIKey, crypto.SettingAAD(ws.ID().String(), crypto.FieldEmbeddingKey))
	if err != nil {
		h.fail(w, "sealing embedding key", err)
		return
	}
	h.updateSettings(w, r, ws, "embedding credential verified and stored", func(s *entity.WorkspaceSettings) error {
		return s.SetEmbeddings("openai", sealed, opts.Model, opts.BaseURL)
	})
}

type notificationRequest struct {
	Provider   string `json:"provider"`
	WebhookURL string `json:"webhook_url"`
}

// SetNotifications stores where this tenant's investigations are posted.
func (h *WorkspaceHandler) SetNotifications(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.authenticated(w, r)
	if !ok {
		return
	}

	var req notificationRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	provider, err := notify.ParseProvider(req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var sealed []byte
	// slack_bot posts through the installed app, so it has no webhook to validate.
	if provider != notify.ProviderNone && provider != notify.ProviderSlackBot {
		if _, err := notify.New(provider, req.WebhookURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if sealed, err = h.cipher.Seal(req.WebhookURL, crypto.SettingAAD(ws.ID().String(), crypto.FieldNotifyWebhook)); err != nil {
			h.fail(w, "sealing webhook url", err)
			return
		}
	}

	h.updateSettings(w, r, ws, "notification destination stored", func(s *entity.WorkspaceSettings) error {
		return s.SetNotificationTarget(provider.String(), sealed)
	})
}

type onCallRequest struct {
	Provider   string `json:"provider"`
	APIKey     string `json:"api_key"`
	ScheduleID string `json:"schedule_id"`
}

// SetOnCall stores where to look up who is on call, after proving the key can
// read the schedule. Those people are invited to each incident channel.
func (h *WorkspaceHandler) SetOnCall(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.authenticated(w, r)
	if !ok {
		return
	}

	var req onCallRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	provider, err := oncall.ParseProvider(req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var sealed []byte
	if provider != oncall.ProviderNone {
		if req.APIKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_key is required"})
			return
		}
		lister, err := oncall.New(provider, req.APIKey, req.ScheduleID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// Verify before storing, so a wrong key or schedule fails now, not mid-incident.
		if _, err := lister.OnCallEmails(r.Context()); err != nil {
			h.log.Warn().Err(err).Msg("on-call credential check failed")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the on-call credential or schedule was rejected"})
			return
		}
		if sealed, err = h.cipher.Seal(req.APIKey, crypto.SettingAAD(ws.ID().String(), crypto.FieldOnCallKey)); err != nil {
			h.fail(w, "sealing on-call key", err)
			return
		}
	}

	h.updateSettings(w, r, ws, "on-call source stored", func(s *entity.WorkspaceSettings) error {
		return s.SetOnCall(provider.String(), sealed, req.ScheduleID)
	})
}

// RotateAlertToken issues a new ingestion credential, invalidating the previous one.
func (h *WorkspaceHandler) RotateAlertToken(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	// Reload and save under optimistic concurrency: a copy loaded before a
	// concurrent change (a Slack reinstall) must not overwrite it.
	var token string
	err := repository.RetryOnConflict(r.Context(), func() error {
		fresh, err := h.workspaces.GetByID(r.Context(), ws.ID())
		if err != nil {
			return err
		}
		if token, err = fresh.GenerateWebhookSecret(); err != nil {
			return err
		}
		return h.workspaces.Update(r.Context(), fresh)
	})
	if err != nil {
		h.fail(w, "rotating alert token", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"alert_token": token,
		"note":        "The previous token no longer works. Update your monitoring integrations.",
	})
}

// TestNotification posts a sample message so a customer can confirm delivery
// without waiting for a real outage.
func (h *WorkspaceHandler) TestNotification(w http.ResponseWriter, r *http.Request) {
	ws, settings, ok := h.authenticated(w, r)
	if !ok {
		return
	}

	if settings.UsesIncidentChannels() {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "this workspace posts to per-incident Slack channels; run the setup check to see one open",
		})
		return
	}

	notifier, err := h.resolver.ResolveNotifier(r.Context(), ws.ID())
	if err != nil {
		h.log.Error().Err(err).Msg("resolving notifier")
		msg := "could not prepare notifications for this workspace"
		if errors.Is(err, tenant.ErrNotConfigured) {
			msg = "set a notification destination first"
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}

	if err := notifier.Notify(r.Context(), sampleReport(ws.SlackTeamName())); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "delivery failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "test notification delivered"})
}

// sampleReport is a clearly-labelled example so nobody mistakes it for an incident.
func sampleReport(workspaceName string) notify.Report {
	return notify.Report{
		IncidentTitle:  "Test notification from criAIsis",
		Severity:       "sev-4",
		Trigger:        "manual",
		Consensus:      "This is a test message confirming that " + workspaceName + " is connected. No incident is in progress.",
		Classification: "code",
		OwningDomain:   "application",
		NextSteps:      []string{"Connect Grafana or CloudWatch to start receiving real investigations"},
		Elapsed:        "0s",
	}
}

// authenticated resolves the tenant and their settings for a management request.
func (h *WorkspaceHandler) authenticated(w http.ResponseWriter, r *http.Request) (*entity.Workspace, *entity.WorkspaceSettings, bool) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return nil, nil, false
	}

	settings, err := h.settings.Get(r.Context(), ws.ID())
	if err != nil {
		h.fail(w, "loading settings", err)
		return nil, nil, false
	}
	return ws, settings, true
}

// updateSettings applies one change to a fresh copy of the tenant's settings.
//
// The handlers verify credentials against an external service first, which can
// take many seconds, so the row is re-read only after that slow work and saved
// under optimistic concurrency: a write that raced another is retried on fresh
// data rather than overwriting it. This holds across replicas, unlike a lock.
func (h *WorkspaceHandler) updateSettings(
	w http.ResponseWriter, r *http.Request, ws *entity.Workspace, message string,
	apply func(*entity.WorkspaceSettings) error,
) {
	var settings *entity.WorkspaceSettings
	var invalid error
	err := repository.RetryOnConflict(r.Context(), func() error {
		var err error
		if settings, err = h.settings.Get(r.Context(), ws.ID()); err != nil {
			return err
		}
		if invalid = apply(settings); invalid != nil {
			return nil // a validation error is the caller's, not a conflict to retry
		}
		return h.settings.Update(r.Context(), settings)
	})
	switch {
	case invalid != nil:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": invalid.Error()})
	case err != nil:
		h.fail(w, "saving settings", err)
	default:
		h.report(w, settings, message)
	}
}

// report tells the caller what remains before investigations can run.
func (h *WorkspaceHandler) report(w http.ResponseWriter, settings *entity.WorkspaceSettings, message string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":               message,
		"ready_to_investigate": settings.IsReadyToInvestigate(),
		"missing":              settings.MissingRequirements(),
	})
}

// fail logs the cause and returns a generic message, so an internal error never
// leaks storage or credential detail to a caller.
func (h *WorkspaceHandler) fail(w http.ResponseWriter, action string, err error) {
	h.log.Error().Err(err).Msg(action)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("could not complete: %s", action)})
}

// decodeJSON reads a JSON body capped at maxBodyBytes.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeJSONLimit(w, r, target, maxBodyBytes)
}

// decodeJSONLimit reads a JSON body capped at limit bytes. An oversize body is a
// 413, not a truncated parse that surfaces as "not valid json".
func decodeJSONLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("request body exceeds %d bytes", limit)})
			return false
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read request body"})
		return false
	}
	if err := json.Unmarshal(body, target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body is not valid json"})
		return false
	}
	return true
}
