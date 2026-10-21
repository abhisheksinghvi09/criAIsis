package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/handler"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/service/tenant"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const platformKey = "platform-admin-key-long-enough-here"

// multiWorkspaces holds several tenants, so cross-tenant access can be attempted.
type multiWorkspaces struct {
	mu     sync.Mutex
	byTeam map[string]*entity.Workspace
}

func (m *multiWorkspaces) Create(_ context.Context, ws *entity.Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byTeam[ws.SlackTeamID()] = ws
	return nil
}

func (m *multiWorkspaces) Update(_ context.Context, ws *entity.Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byTeam[ws.SlackTeamID()] = ws
	return nil
}

func (m *multiWorkspaces) GetBySlackTeamID(_ context.Context, teamID string) (*entity.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ws, ok := m.byTeam[teamID]; ok {
		return ws, nil
	}
	return nil, errors.New("workspace not found")
}

func (m *multiWorkspaces) GetByID(_ context.Context, id value.WorkspaceID) (*entity.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ws := range m.byTeam {
		if ws.ID() == id {
			return ws, nil
		}
	}
	return nil, errors.New("workspace not found")
}

// recordingSettings tracks which workspace's settings were read or written, which
// is how a cross-tenant leak would be detected.
type recordingSettings struct {
	mu       sync.Mutex
	byWS     map[value.WorkspaceID]*entity.WorkspaceSettings
	lastRead value.WorkspaceID
}

func (s *recordingSettings) Create(_ context.Context, settings *entity.WorkspaceSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byWS[settings.WorkspaceID()] = settings
	return nil
}

func (s *recordingSettings) Update(_ context.Context, settings *entity.WorkspaceSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byWS[settings.WorkspaceID()] = settings
	return nil
}

func (s *recordingSettings) Get(_ context.Context, wsID value.WorkspaceID) (*entity.WorkspaceSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRead = wsID
	if settings, ok := s.byWS[wsID]; ok {
		return settings, nil
	}
	return nil, errors.New("settings not found")
}

type stubPersonas struct{}

func (stubPersonas) Create(context.Context, *entity.Persona) error { return nil }
func (stubPersonas) Update(context.Context, *entity.Persona) error { return nil }
func (stubPersonas) GetByID(context.Context, value.WorkspaceID, uuid.UUID) (*entity.Persona, error) {
	return nil, errors.New("not implemented")
}
func (stubPersonas) GetByKey(context.Context, value.WorkspaceID, value.PersonaKey) (*entity.Persona, error) {
	return nil, errors.New("not implemented")
}
func (stubPersonas) ListByWorkspace(context.Context, value.WorkspaceID) ([]*entity.Persona, error) {
	return nil, nil
}

var (
	_ repository.PersonaRepository   = stubPersonas{}
	_ repository.WorkspaceRepository = (*multiWorkspaces)(nil)
	_ repository.SettingsRepository  = (*recordingSettings)(nil)
)

type mgmt struct {
	router   http.Handler
	settings *recordingSettings
	keys     map[string]string // team id -> admin key
}

// newMgmt builds the management API with two tenants already provisioned.
func newMgmt(t *testing.T, teams ...string) *mgmt {
	t.Helper()
	logger := zerolog.New(io.Discard)

	cipher, err := crypto.New("01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("building cipher: %v", err)
	}

	workspaces := &multiWorkspaces{byTeam: map[string]*entity.Workspace{}}
	settings := &recordingSettings{byWS: map[value.WorkspaceID]*entity.WorkspaceSettings{}}
	keys := map[string]string{}

	for _, team := range teams {
		ws, err := entity.NewWorkspace(team, team, []byte("placeholder"))
		if err != nil {
			t.Fatalf("building workspace: %v", err)
		}
		adminKey, err := ws.GenerateAdminAPIKey()
		if err != nil {
			t.Fatalf("generating admin key: %v", err)
		}
		_ = workspaces.Create(context.Background(), ws)
		keys[team] = adminKey

		s, err := entity.NewWorkspaceSettings(ws.ID(), entity.SettingsDefaults{})
		if err != nil {
			t.Fatalf("building settings: %v", err)
		}
		_ = settings.Create(context.Background(), s)
	}

	resolver := tenant.NewResolver(settings, cipher)
	wh := handler.NewWorkspaceHandler(workspaces, stubPersonas{}, settings, noopTxManager{}, resolver, nil, cipher, entity.SettingsDefaults{}, &logger)
	auth := handler.NewAuth(workspaces, platformKey, &logger)

	r := chi.NewRouter()
	r.Route("/api/v1/workspaces", func(ws chi.Router) {
		ws.With(auth.RequirePlatformAdmin).Post("/", wh.Create)
		ws.Route("/{workspace}", func(one chi.Router) {
			one.Use(auth.RequireWorkspaceAdmin)
			one.Get("/", wh.Show)
			one.Put("/notifications", wh.SetNotifications)
		})
	})

	return &mgmt{router: r, settings: settings, keys: keys}
}

func (m *mgmt) do(t *testing.T, method, target, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	return rec
}

// The core multi-tenancy guarantee: a valid credential for one tenant must not
// grant access to another. Holding *a* key is not enough; it must be the right one.
func TestTenancy_AdminKeyIsNotPortableAcrossWorkspaces(t *testing.T) {
	m := newMgmt(t, "alpha", "beta")

	rec := m.do(t, http.MethodGet, "/api/v1/workspaces/beta/", m.keys["alpha"], "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("alpha's key reached beta: got %d", rec.Code)
	}
	if m.settings.lastRead != (value.WorkspaceID{}) {
		t.Error("beta's settings were read during a rejected request")
	}
}

func TestTenancy_AdminKeyReachesItsOwnWorkspace(t *testing.T) {
	m := newMgmt(t, "alpha", "beta")

	rec := m.do(t, http.MethodGet, "/api/v1/workspaces/alpha/", m.keys["alpha"], "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A write must land on the authenticated tenant, never the one named elsewhere.
func TestTenancy_WriteCannotTargetAnotherWorkspace(t *testing.T) {
	m := newMgmt(t, "alpha", "beta")

	rec := m.do(t, http.MethodPut, "/api/v1/workspaces/beta/notifications", m.keys["alpha"],
		`{"provider":"slack","webhook_url":"https://hooks.slack.com/services/T/B/x"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("alpha wrote to beta's settings: got %d", rec.Code)
	}
}

// Secrets must never come back out of the API.
func TestTenancy_ShowNeverReturnsSecrets(t *testing.T) {
	m := newMgmt(t, "alpha")

	set := m.do(t, http.MethodPut, "/api/v1/workspaces/alpha/notifications", m.keys["alpha"],
		`{"provider":"discord","webhook_url":"https://discord.com/api/webhooks/1/super-secret-value"}`)
	if set.Code != http.StatusOK {
		t.Fatalf("setting destination failed: %d %s", set.Code, set.Body.String())
	}

	rec := m.do(t, http.MethodGet, "/api/v1/workspaces/alpha/", m.keys["alpha"], "")
	body := rec.Body.String()

	if strings.Contains(body, "super-secret-value") {
		t.Error("the webhook url was returned by the API")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("response is not json: %v", err)
	}
	notifications, _ := payload["notifications"].(map[string]any)
	if notifications["webhook_set"] != true {
		t.Error("expected webhook_set to report that a destination is configured")
	}
}

// Creating a tenant is a platform operation: a workspace admin key must not do it.
func TestTenancy_WorkspaceKeyCannotCreateWorkspaces(t *testing.T) {
	m := newMgmt(t, "alpha")

	rec := m.do(t, http.MethodPost, "/api/v1/workspaces/", m.keys["alpha"], `{"team_id":"gamma","name":"Gamma"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a workspace key created a workspace: got %d", rec.Code)
	}
}

func TestTenancy_PlatformKeyCreatesWorkspaces(t *testing.T) {
	m := newMgmt(t, "alpha")

	rec := m.do(t, http.MethodPost, "/api/v1/workspaces/", platformKey, `{"team_id":"gamma","name":"Gamma"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload["admin_key"] == "" || payload["alert_token"] == "" {
		t.Error("expected both credentials to be returned once at creation")
	}
	if payload["admin_key"] == payload["alert_token"] {
		t.Error("the admin key and the alert token must be distinct credentials")
	}
}

func TestTenancy_RejectsMissingAndMalformedCredentials(t *testing.T) {
	m := newMgmt(t, "alpha")

	cases := map[string]string{"absent": "", "wrong": "not-a-real-key", "empty bearer": " "}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := m.do(t, http.MethodGet, "/api/v1/workspaces/alpha/", key, ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rec.Code)
			}
		})
	}
}
