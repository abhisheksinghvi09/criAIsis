package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
	"criaisis/internal/handler"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/slack"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/orchestrator"
	"criaisis/internal/service/tenant"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fakeSettingsRepo struct{ created int }

func (f *fakeSettingsRepo) Create(context.Context, *entity.WorkspaceSettings) error {
	f.created++
	return nil
}
func (f *fakeSettingsRepo) Get(context.Context, value.WorkspaceID) (*entity.WorkspaceSettings, error) {
	return nil, nil
}
func (f *fakeSettingsRepo) Update(context.Context, *entity.WorkspaceSettings) error { return nil }

type fakePersonaRepo struct{ created int }

func (f *fakePersonaRepo) Create(context.Context, *entity.Persona) error {
	f.created++
	return nil
}
func (f *fakePersonaRepo) GetByID(context.Context, value.WorkspaceID, uuid.UUID) (*entity.Persona, error) {
	return nil, nil
}
func (f *fakePersonaRepo) GetByKey(context.Context, value.WorkspaceID, value.PersonaKey) (*entity.Persona, error) {
	return nil, nil
}
func (f *fakePersonaRepo) ListByWorkspace(context.Context, value.WorkspaceID) ([]*entity.Persona, error) {
	return nil, nil
}
func (f *fakePersonaRepo) Update(context.Context, *entity.Persona) error { return nil }

type passthroughTx struct{}

func (passthroughTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

var testProvSettings, testProvPersonas = &fakeSettingsRepo{}, &fakePersonaRepo{}

func newTestProvisioner(ws *mockWorkspaceRepo) *handler.Provisioner {
	return handler.NewProvisioner(ws, testProvSettings, testProvPersonas, passthroughTx{}, entity.SettingsDefaults{})
}

func newOAuthHandler(t *testing.T, wsRepo *mockWorkspaceRepo) *handler.SlackInboundHandler {
	t.Helper()
	logger := zerolog.Nop()
	cipher, _ := crypto.New("01234567890123456789012345678901")
	resolver := tenant.NewResolver(nil, cipher)
	incSvc := incident.New(newMockIncidentRepo(), resolver, func(*tenant.Runtime) *orchestrator.Orchestrator { return nil }, &mockQueue{}, &logger)
	sl := &mockSlackClient{oauthResp: &slack.OAuthResponse{OK: true, AccessToken: "xoxb-t"}}
	sl.oauthResp.Team.ID, sl.oauthResp.Team.Name = "T999", "New Team"
	return handler.NewSlackInboundHandler(wsRepo, newMockIncidentRepo(), incSvc, resolver, nil, cipher, sl,
		newTestProvisioner(wsRepo), "client-id", "client-secret", &logger)
}

// oauthCallbackRequest runs /slack/install and returns a callback request that
// carries the state and cookie it issued, as a real browser would.
func oauthCallbackRequest(t *testing.T, h *handler.SlackInboundHandler, code string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleInstall(rec, httptest.NewRequest("GET", "/slack/install", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("install status = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/slack/oauth/callback?code="+code+"&state="+url.QueryEscape(loc.Query().Get("state")), nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req, httptest.NewRecorder()
}

func TestOAuthCallback_RejectsMissingOrForgedState(t *testing.T) {
	wsRepo := newMockWorkspaceRepo()
	h := newOAuthHandler(t, wsRepo)

	for name, target := range map[string]string{
		"no state":     "/slack/oauth/callback?code=c",
		"forged state": "/slack/oauth/callback?code=c&state=aa.9999999999.bb",
	} {
		rec := httptest.NewRecorder()
		h.HandleOAuthCallback(rec, httptest.NewRequest("GET", target, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}

	// A valid state issued to someone else's browser (no matching cookie) fails too.
	req, rec := oauthCallbackRequest(t, h, "c")
	req.Header.Del("Cookie")
	h.HandleOAuthCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("state without cookie: status = %d, want 400", rec.Code)
	}
	if len(wsRepo.workspaces) != 0 {
		t.Errorf("rejected installs created %d workspaces", len(wsRepo.workspaces))
	}
}

func TestOAuthCallback_NewInstallProvisionsACompleteTenant(t *testing.T) {
	wsRepo := newMockWorkspaceRepo()
	h := newOAuthHandler(t, wsRepo)
	before := testProvSettings.created

	req, rec := oauthCallbackRequest(t, h, "c")
	h.HandleOAuthCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if testProvSettings.created != before+1 || testProvPersonas.created == 0 {
		t.Errorf("settings/personas not provisioned: settings=%d personas=%d", testProvSettings.created-before, testProvPersonas.created)
	}
	ws := wsRepo.workspaces["T999"]
	if ws == nil {
		t.Fatal("workspace not created")
	}
	key := between(rec.Body.String(), "Admin key (dashboard sign-in): ", "\n")
	if key == "" || !ws.VerifyAdminAPIKey(key) {
		t.Errorf("admin key shown to installer does not authenticate (key %q)", key)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("credentials page must not be cacheable")
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return s
}
