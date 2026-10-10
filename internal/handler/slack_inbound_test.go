package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
	"criaisis/internal/handler"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/slack"
	"criaisis/internal/job"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/orchestrator"
	"criaisis/internal/service/tenant"

	"github.com/rs/zerolog"
)

type mockWorkspaceRepo struct {
	workspaces map[string]*entity.Workspace
}

func newMockWorkspaceRepo() *mockWorkspaceRepo {
	return &mockWorkspaceRepo{workspaces: make(map[string]*entity.Workspace)}
}

func (m *mockWorkspaceRepo) Create(ctx context.Context, ws *entity.Workspace) error {
	m.workspaces[ws.SlackTeamID()] = ws
	return nil
}

func (m *mockWorkspaceRepo) GetByID(ctx context.Context, id value.WorkspaceID) (*entity.Workspace, error) {
	for _, ws := range m.workspaces {
		if ws.ID() == id {
			return ws, nil
		}
	}
	return nil, nil
}

func (m *mockWorkspaceRepo) GetBySlackTeamID(ctx context.Context, teamID string) (*entity.Workspace, error) {
	ws, ok := m.workspaces[teamID]
	if !ok {
		return nil, nil
	}
	return ws, nil
}

func (m *mockWorkspaceRepo) Update(ctx context.Context, ws *entity.Workspace) error {
	m.workspaces[ws.SlackTeamID()] = ws
	return nil
}

type mockSlackClient struct {
	oauthResp    *slack.OAuthResponse
	postedText   string
	postedTarget string
}

func (m *mockSlackClient) ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code string) (*slack.OAuthResponse, error) {
	return m.oauthResp, nil
}

func (m *mockSlackClient) PostMessage(ctx context.Context, token, channel, threadTS, text string) error {
	m.postedText = text
	m.postedTarget = channel
	return nil
}

func (m *mockSlackClient) PostEphemeral(ctx context.Context, token, channel, userID, text string) error {
	m.postedText = text
	m.postedTarget = channel
	return nil
}

type mockQueue struct{}

func (q *mockQueue) Enqueue(j job.IncidentJob) error { return nil }
func (q *mockQueue) Start(ctx context.Context, handler job.Handler) {
}
func (q *mockQueue) Shutdown(ctx context.Context) error { return nil }

func TestSlackInboundHandler(t *testing.T) {
	logger := zerolog.Nop()
	cipher, _ := crypto.New("01234567890123456789012345678901") // 32 bytes
	wsRepo := newMockWorkspaceRepo()
	incRepo := newMockIncidentRepo()
	queue := &mockQueue{}

	buildOrch := func(rt *tenant.Runtime) *orchestrator.Orchestrator {
		return nil
	}
	resolver := tenant.NewResolver(nil, cipher)
	incSvc := incident.New(incRepo, resolver, buildOrch, queue, &logger)

	slClient := &mockSlackClient{
		oauthResp: &slack.OAuthResponse{
			OK:          true,
			AccessToken: "xoxb-test-token",
			Team: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{
				ID:   "T123",
				Name: "Acme Corp",
			},
		},
	}

	h := handler.NewSlackInboundHandler(
		wsRepo, incRepo, incSvc, resolver, buildOrch, cipher, slClient,
		"client-id", "client-secret", &logger,
	)

	t.Run("BR1.1: OAuth denial creates no workspace", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/slack/oauth/callback?error=access_denied", nil)
		rec := httptest.NewRecorder()

		h.HandleOAuthCallback(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if len(wsRepo.workspaces) != 0 {
			t.Errorf("expected 0 workspaces, got %d", len(wsRepo.workspaces))
		}
	})

	t.Run("US1.1: OAuth success creates workspace with encrypted token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/slack/oauth/callback?code=test-code", nil)
		rec := httptest.NewRecorder()

		h.HandleOAuthCallback(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		ws, _ := wsRepo.GetBySlackTeamID(context.Background(), "T123")
		if ws == nil {
			t.Fatal("expected workspace T123 to exist")
		}
		if ws.SlackTeamName() != "Acme Corp" {
			t.Errorf("expected Acme Corp, got %s", ws.SlackTeamName())
		}
		if len(ws.SlackBotTokenEncrypted()) == 0 {
			t.Error("expected encrypted token")
		}
	})

	t.Run("BR2.1: /criaisis investigate requires description", func(t *testing.T) {
		form := url.Values{
			"command":    {"/criaisis"},
			"text":       {"investigate"},
			"channel_id": {"C123"},
			"user_id":    {"U123"},
			"team_id":    {"T123"},
		}
		req := httptest.NewRequest("POST", "/slack/commands", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		h.HandleCommand(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Description is required") {
			t.Errorf("expected error message in response, got %s", rec.Body.String())
		}
	})

	t.Run("US2.1 and BR2.2: /criaisis investigate acknowledges in <500ms", func(t *testing.T) {
		form := url.Values{
			"command":    {"/criaisis"},
			"text":       {"investigate db connection pool spikes sev-2"},
			"channel_id": {"C123"},
			"user_id":    {"U123"},
			"team_id":    {"T123"},
		}
		req := httptest.NewRequest("POST", "/slack/commands", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		start := time.Now()
		h.HandleCommand(rec, req)
		elapsed := time.Since(start)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if elapsed > 500*time.Millisecond {
			t.Errorf("expected response in <500ms, took %v", elapsed)
		}
		if !strings.Contains(rec.Body.String(), "Investigation started") {
			t.Errorf("expected confirmation in body, got %s", rec.Body.String())
		}
	})

	t.Run("Events API url_verification handshake", func(t *testing.T) {
		body := map[string]string{
			"type":      "url_verification",
			"challenge": "challenge_token_xyz",
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/slack/events", strings.NewReader(string(raw)))
		rec := httptest.NewRecorder()

		h.HandleEvents(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "challenge_token_xyz") {
			t.Errorf("expected challenge token in response, got %s", rec.Body.String())
		}
	})
}
