package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/slack"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/tenant"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// SlackInboundHandler processes all inbound interactions from Slack.
type SlackInboundHandler struct {
	workspaces   repository.WorkspaceRepository
	incidents    repository.IncidentRepository
	incidentSvc  *incident.Service
	resolver     *tenant.Resolver
	build        incident.BuildOrchestrator
	crypto       *crypto.Cipher
	slackClient  slack.Client
	clientID     string
	clientSecret string
	log          *zerolog.Logger
}

// NewSlackInboundHandler constructs the Slack inbound handler.
func NewSlackInboundHandler(
	workspaces repository.WorkspaceRepository,
	incidents repository.IncidentRepository,
	incidentSvc *incident.Service,
	resolver *tenant.Resolver,
	build incident.BuildOrchestrator,
	crypto *crypto.Cipher,
	slackClient slack.Client,
	clientID string,
	clientSecret string,
	log *zerolog.Logger,
) *SlackInboundHandler {
	return &SlackInboundHandler{
		workspaces:   workspaces,
		incidents:    incidents,
		incidentSvc:  incidentSvc,
		resolver:     resolver,
		build:        build,
		crypto:       crypto,
		slackClient:  slackClient,
		clientID:     clientID,
		clientSecret: clientSecret,
		log:          log,
	}
}

// HandleOAuthCallback handles the OAuth 2.0 exchange when an admin installs the app.
func (h *SlackInboundHandler) HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check if user or Slack denied installation (BR1.1)
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		h.log.Warn().Str("error", errParam).Msg("slack oauth install denied")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Slack installation was cancelled or denied. You can try again."))
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing authorization code"})
		return
	}

	res, err := h.slackClient.ExchangeOAuthCode(ctx, h.clientID, h.clientSecret, code)
	if err != nil {
		h.log.Error().Err(err).Msg("failed exchanging slack oauth code")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "oauth exchange failed"})
		return
	}

	encryptedToken, err := h.crypto.Encrypt(res.AccessToken)
	if err != nil {
		h.log.Error().Err(err).Msg("failed encrypting slack bot token")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encryption failed"})
		return
	}

	ws, err := h.workspaces.GetBySlackTeamID(ctx, res.Team.ID)
	if err == nil && ws != nil {
		// Existing workspace reinstall: update team name and bot token
		_ = ws.UpdateTeamName(res.Team.Name)
		_ = ws.UpdateBotToken(encryptedToken)
		if err := h.workspaces.Update(ctx, ws); err != nil {
			h.log.Error().Err(err).Msg("failed updating workspace on reinstall")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed updating workspace"})
			return
		}
	} else {
		// New installation
		newWS, err := entity.NewWorkspace(res.Team.ID, res.Team.Name, encryptedToken)
		if err != nil {
			h.log.Error().Err(err).Msg("failed creating workspace entity")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed creating workspace"})
			return
		}
		if err := h.workspaces.Create(ctx, newWS); err != nil {
			h.log.Error().Err(err).Msg("failed saving new workspace")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed saving workspace"})
			return
		}
	}

	h.log.Info().Str("team_id", res.Team.ID).Str("team_name", res.Team.Name).Msg("slack app installed successfully")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Slack installation successful! You may return to the criAIsis dashboard."))
}

// HandleCommand processes slash commands (/criaisis investigate, /criaisis resolve).
func (h *SlackInboundHandler) HandleCommand(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed form data"})
		return
	}

	cmd := r.Form.Get("command")
	text := strings.TrimSpace(r.Form.Get("text"))
	channelID := r.Form.Get("channel_id")
	userID := r.Form.Get("user_id")
	teamID := r.Form.Get("team_id")

	if cmd != "/criaisis" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown command"})
		return
	}

	parts := strings.Fields(text)
	if len(parts) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{
			"response_type": "ephemeral",
			"text":          "Usage: /criaisis investigate <description> [sev-1|sev-2|sev-3|sev-4]\n       /criaisis resolve",
		})
		return
	}

	subcommand := parts[0]
	switch subcommand {
	case "investigate":
		h.handleInvestigate(w, r, teamID, channelID, userID, parts[1:])
	case "resolve":
		h.handleResolve(w, r, teamID, channelID)
	default:
		writeJSON(w, http.StatusOK, map[string]string{
			"response_type": "ephemeral",
			"text":          fmt.Sprintf("Unknown subcommand '%s'. Valid subcommands are 'investigate' and 'resolve'.", subcommand),
		})
	}
}

func (h *SlackInboundHandler) handleInvestigate(
	w http.ResponseWriter,
	r *http.Request,
	teamID string,
	channelID string,
	userID string,
	args []string,
) {
	if len(args) == 0 {
		// BR2.1: description required
		writeJSON(w, http.StatusOK, map[string]string{
			"response_type": "ephemeral",
			"text":          "Error: Description is required. Usage: /criaisis investigate <description> [severity]",
		})
		return
	}

	// Parse optional severity at the end
	severity := value.SeveritySev3
	descWords := args
	lastWord := strings.ToLower(args[len(args)-1])
	if parsedSev, err := value.ParseSeverity(lastWord); err == nil && len(args) > 1 {
		severity = parsedSev
		descWords = args[:len(args)-1]
	}
	description := strings.Join(descWords, " ")

	// BR2.2: Acknowledge immediately (<500ms)
	writeJSON(w, http.StatusOK, map[string]string{
		"response_type": "ephemeral",
		"text":          fmt.Sprintf("Investigation started (%s): %s", severity, description),
	})

	// Asynchronously enqueue debate job
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				h.log.Error().
					Str("team_id", teamID).
					Str("channel_id", channelID).
					Interface("panic", rec).
					Msg("panic during async incident investigation dispatch (NFR4)")
			}
		}()

		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		ws, err := h.workspaces.GetBySlackTeamID(bgCtx, teamID)
		if err != nil {
			h.log.Error().Err(err).Str("team_id", teamID).Msg("job dispatch failed: workspace lookup failed (BR7.4)")
			return
		}

		chanID, err := value.ParseSlackChannelID(channelID)
		if err != nil {
			h.log.Error().Err(err).Str("team_id", teamID).Str("channel_id", channelID).
				Msg("job dispatch failed: invalid slack channel id (BR7.4)")
			return
		}
		uID, err := value.ParseSlackUserID(userID)
		if err != nil {
			h.log.Error().Err(err).Str("team_id", teamID).Str("user_id", userID).
				Msg("job dispatch failed: invalid slack user id (BR7.4)")
			return
		}
		threadTS, err := value.ParseSlackThreadTS(fmt.Sprintf("%d.000000", time.Now().UTC().UnixNano()/1000))
		if err != nil {
			h.log.Error().Err(err).Str("team_id", teamID).
				Msg("job dispatch failed: invalid generated thread timestamp (BR7.4)")
			return
		}

		input := incident.NewIncidentInput{
			WorkspaceID: ws.ID(),
			Title:       description,
			Description: description,
			Severity:    severity,
			Trigger:     value.TriggerTypeSlashCommand,
			ChannelID:   chanID,
			ThreadTS:    threadTS,
			CreatedBy:   uID,
		}

		if _, err := h.incidentSvc.Open(bgCtx, input); err != nil {
			h.log.Error().Err(err).
				Str("workspace_id", ws.ID().String()).
				Str("team_id", teamID).
				Msg("job dispatch failed: incident open/enqueue failed (BR7.4)")
			return
		}
	}()
}

func (h *SlackInboundHandler) handleResolve(w http.ResponseWriter, r *http.Request, teamID string, channelID string) {
	ctx := r.Context()
	ws, err := h.workspaces.GetBySlackTeamID(ctx, teamID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"response_type": "ephemeral",
			"text":          "Workspace not found.",
		})
		return
	}

	activeIncidents, err := h.incidents.ListActive(ctx, ws.ID(), 1, 0)
	if err != nil || len(activeIncidents) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{
			"response_type": "ephemeral",
			"text":          "No active incident found to resolve.",
		})
		return
	}

	inc := activeIncidents[0]
	if _, err := h.incidentSvc.Resolve(ctx, ws.ID(), inc.ID()); err != nil {
		if errors.Is(err, entity.ErrIncidentAlreadyResolved) {
			writeJSON(w, http.StatusOK, map[string]string{
				"response_type": "ephemeral",
				"text":          "Incident is already resolved.",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"response_type": "ephemeral",
			"text":          fmt.Sprintf("Failed resolving incident: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"response_type": "in_channel",
		"text":          fmt.Sprintf("Incident '%s' has been marked resolved. Transcript locked.", inc.Title()),
	})
}

// HandleInteractivity processes Slack Block Kit interactive actions (US2.3).
func (h *SlackInboundHandler) HandleInteractivity(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed form data"})
		return
	}

	rawPayload := r.Form.Get("payload")
	if rawPayload == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing payload"})
		return
	}

	var payload struct {
		Type string `json:"type"`
		Team struct {
			ID string `json:"id"`
		} `json:"team"`
		Actions []struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		} `json:"actions"`
	}

	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload json"})
		return
	}

	for _, action := range payload.Actions {
		switch action.ActionID {
		case "resolve":
			if action.Value == "" {
				continue
			}
			incUUID, err := uuid.Parse(action.Value)
			if err != nil {
				h.log.Error().Err(err).Str("value", action.Value).Msg("resolve action: invalid incident id (BR7.4)")
				continue
			}
			ws, err := h.workspaces.GetBySlackTeamID(r.Context(), payload.Team.ID)
			if err != nil || ws == nil {
				h.log.Error().Err(err).Str("team_id", payload.Team.ID).Msg("resolve action: workspace lookup failed (BR7.4)")
				continue
			}
			if _, err := h.incidentSvc.Resolve(r.Context(), ws.ID(), incUUID); err != nil {
				h.log.Error().Err(err).
					Str("workspace_id", ws.ID().String()).
					Str("incident_id", incUUID.String()).
					Msg("resolve action: resolving incident failed (BR7.4)")
			}
		case "mention_specialist":
			// Client-side action: Instruct Slack user
			writeJSON(w, http.StatusOK, map[string]any{"response_action": "clear"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleEvents processes Slack Events API events (URL verification & app_mention follow-ups).
func (h *SlackInboundHandler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		TeamID    string `json:"team_id"`
		Event     struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Channel  string `json:"channel"`
			ThreadTS string `json:"thread_ts"`
			User     string `json:"user"`
		} `json:"event"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed json"})
		return
	}

	// Handshake challenge response
	if body.Type == "url_verification" {
		writeJSON(w, http.StatusOK, map[string]string{"challenge": body.Challenge})
		return
	}

	if body.Type == "event_callback" && body.Event.Type == "app_mention" {
		h.handleAppMention(w, r, body.TeamID, body.Event.Channel, body.Event.ThreadTS, body.Event.Text)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
}

func (h *SlackInboundHandler) handleAppMention(
	w http.ResponseWriter,
	r *http.Request,
	teamID string,
	channelID string,
	threadTS string,
	text string,
) {
	// Acknowledge event immediately to prevent Slack delivery retries
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				h.log.Error().Interface("panic", rec).Msg("panic in app_mention handling")
			}
		}()

		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		ws, err := h.workspaces.GetBySlackTeamID(bgCtx, teamID)
		if err != nil {
			h.log.Error().Err(err).Msg("workspace not found for app_mention")
			return
		}

		chanID, err := value.ParseSlackChannelID(channelID)
		if err != nil {
			h.log.Error().Err(err).Str("channel_id", channelID).Msg("app_mention: invalid slack channel id")
			return
		}
		tTS, err := value.ParseSlackThreadTS(threadTS)
		if err != nil {
			h.log.Error().Err(err).Str("thread_ts", threadTS).Msg("app_mention: invalid slack thread timestamp")
			return
		}

		inc, err := h.incidents.GetBySlackThread(bgCtx, ws.ID(), chanID, tTS)
		if err != nil || inc == nil {
			h.log.Warn().Str("thread_ts", threadTS).Msg("incident not found for thread follow-up")
			return
		}

		// Detect target persona from text (e.g. "@database", "@network", "@security", "@code")
		personaKey := value.PersonaKeyDatabase
		lowerText := strings.ToLower(text)
		if strings.Contains(lowerText, "network") {
			personaKey = value.PersonaKeyNetwork
		} else if strings.Contains(lowerText, "security") {
			personaKey = value.PersonaKeySecurity
		} else if strings.Contains(lowerText, "code") || strings.Contains(lowerText, "app") {
			personaKey = value.PersonaKeyApplication
		}

		rt, err := h.resolver.Resolve(bgCtx, ws.ID())
		if err != nil {
			h.log.Error().Err(err).Msg("resolving tenant runtime for follow-up failed")
			return
		}

		orch := h.build(rt)
		turn, err := orch.FollowUp(bgCtx, inc, personaKey, text)
		if err != nil {
			h.log.Error().Err(err).Msg("follow-up orchestration failed")
			return
		}

		// Decrypt bot token and post response in-thread
		decryptedToken, err := h.crypto.Decrypt(ws.SlackBotTokenEncrypted())
		if err != nil {
			h.log.Error().Err(err).Msg("failed decrypting bot token for reply")
			return
		}

		replyText := fmt.Sprintf("*%s Specialist Diagnosis:*\n%s", strings.Title(personaKey.String()), turn.Content())
		if err := h.slackClient.PostMessage(bgCtx, decryptedToken, channelID, threadTS, replyText); err != nil {
			h.log.Error().Err(err).
				Str("workspace_id", ws.ID().String()).
				Str("channel_id", channelID).
				Msg("app_mention: posting follow-up reply to slack failed")
		}
	}()
}
