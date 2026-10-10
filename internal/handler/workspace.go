package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/llm"
	"criaisis/internal/infrastructure/notify"
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
	resolver   *tenant.Resolver
	incidents  *incident.Service
	cipher     *crypto.Cipher
	defaults   entity.SettingsDefaults
	log        *zerolog.Logger
}

// NewWorkspaceHandler wires the management API.
func NewWorkspaceHandler(
	workspaces repository.WorkspaceRepository,
	personas repository.PersonaRepository,
	settings repository.SettingsRepository,
	resolver *tenant.Resolver,
	incidents *incident.Service,
	cipher *crypto.Cipher,
	defaults entity.SettingsDefaults,
	log *zerolog.Logger,
) *WorkspaceHandler {
	return &WorkspaceHandler{
		workspaces: workspaces, personas: personas, settings: settings, resolver: resolver,
		incidents: incidents, cipher: cipher, defaults: defaults, log: log,
	}
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
	placeholder, err := h.cipher.Encrypt("pending-slack-install")
	if err != nil {
		h.fail(w, "sealing placeholder token", err)
		return
	}

	ws, err := entity.NewWorkspace(req.TeamID, req.Name, placeholder)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	adminKey, err := ws.GenerateAdminAPIKey()
	if err != nil {
		h.fail(w, "generating admin key", err)
		return
	}
	alertToken, err := ws.GenerateWebhookSecret()
	if err != nil {
		h.fail(w, "generating alert token", err)
		return
	}

	if err := h.workspaces.Create(r.Context(), ws); err != nil {
		h.fail(w, "creating workspace", err)
		return
	}

	settings, err := entity.NewWorkspaceSettings(ws.ID(), h.defaults)
	if err != nil {
		h.fail(w, "preparing settings", err)
		return
	}
	if err := h.settings.Create(r.Context(), settings); err != nil {
		h.fail(w, "creating settings", err)
		return
	}

	personas, err := entity.DefaultPersonas(ws.ID())
	if err != nil {
		h.fail(w, "building specialists", err)
		return
	}
	for _, persona := range personas {
		if err := h.personaCreate(r, persona); err != nil {
			h.fail(w, "seeding specialists", err)
			return
		}
	}

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
		"team_id": ws.SlackTeamID(),
		"name":    ws.SlackTeamName(),
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
	_, settings, ok := h.authenticated(w, r)
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

	sealed, err := h.cipher.Encrypt(req.APIKey)
	if err != nil {
		h.fail(w, "sealing model key", err)
		return
	}
	if err := settings.SetLLM("anthropic", sealed, specialist, synthesis); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.save(w, r, settings, "model credential verified and stored")
}

type embeddingRequest struct {
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	BaseURL string `json:"base_url"`
}

// SetEmbeddings stores the tenant's retrieval credential, after proving it works
// and returns the dimension the schema requires.
func (h *WorkspaceHandler) SetEmbeddings(w http.ResponseWriter, r *http.Request) {
	_, settings, ok := h.authenticated(w, r)
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

	sealed, err := h.cipher.Encrypt(req.APIKey)
	if err != nil {
		h.fail(w, "sealing embedding key", err)
		return
	}
	if err := settings.SetEmbeddings("openai", sealed, opts.Model, opts.BaseURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.save(w, r, settings, "embedding credential verified and stored")
}

type notificationRequest struct {
	Provider   string `json:"provider"`
	WebhookURL string `json:"webhook_url"`
}

// SetNotifications stores where this tenant's investigations are posted.
func (h *WorkspaceHandler) SetNotifications(w http.ResponseWriter, r *http.Request) {
	_, settings, ok := h.authenticated(w, r)
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
	if provider != notify.ProviderNone {
		if _, err := notify.New(provider, req.WebhookURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if sealed, err = h.cipher.Encrypt(req.WebhookURL); err != nil {
			h.fail(w, "sealing webhook url", err)
			return
		}
	}

	if err := settings.SetNotificationTarget(provider.String(), sealed); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.save(w, r, settings, "notification destination stored")
}

// RotateAlertToken issues a new ingestion credential, invalidating the previous one.
func (h *WorkspaceHandler) RotateAlertToken(w http.ResponseWriter, r *http.Request) {
	ws, ok := WorkspaceFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	token, err := ws.GenerateWebhookSecret()
	if err != nil {
		h.fail(w, "rotating alert token", err)
		return
	}
	if err := h.workspaces.Update(r.Context(), ws); err != nil {
		h.fail(w, "saving rotated token", err)
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
	ws, _, ok := h.authenticated(w, r)
	if !ok {
		return
	}

	notifier, err := h.resolver.ResolveNotifier(r.Context(), ws.ID())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

// save persists settings and reports what remains before investigations can run.
func (h *WorkspaceHandler) save(w http.ResponseWriter, r *http.Request, settings *entity.WorkspaceSettings, message string) {
	if err := h.settings.Update(r.Context(), settings); err != nil {
		h.fail(w, "saving settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":               message,
		"ready_to_investigate": settings.IsReadyToInvestigate(),
		"missing":              settings.MissingRequirements(),
	})
}

// personaCreate seeds one specialist. Declared separately so Create stays readable.
func (h *WorkspaceHandler) personaCreate(r *http.Request, persona *entity.Persona) error {
	return h.personas.Create(r.Context(), persona)
}

// fail logs the cause and returns a generic message, so an internal error never
// leaks storage or credential detail to a caller.
func (h *WorkspaceHandler) fail(w http.ResponseWriter, action string, err error) {
	h.log.Error().Err(err).Msg(action)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("could not complete: %s", action)})
}

// decodeJSON reads a bounded JSON body, reporting malformed input to the caller.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read request body"})
		return false
	}
	if err := json.Unmarshal(body, target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body is not valid json"})
		return false
	}
	return true
}
