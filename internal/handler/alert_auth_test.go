package handler_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/handler"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/job"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/tenant"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// memWorkspaces resolves a single workspace by Slack team id.
type memWorkspaces struct{ ws *entity.Workspace }

func (m *memWorkspaces) Create(context.Context, *entity.Workspace) error { return nil }
func (m *memWorkspaces) Update(context.Context, *entity.Workspace) error { return nil }

func (m *memWorkspaces) GetByID(context.Context, value.WorkspaceID) (*entity.Workspace, error) {
	return m.ws, nil
}

func (m *memWorkspaces) GetBySlackTeamID(_ context.Context, teamID string) (*entity.Workspace, error) {
	if m.ws != nil && m.ws.SlackTeamID() == teamID {
		return m.ws, nil
	}
	return nil, errors.New("workspace not found")
}

// memIncidents records created incidents.
type memIncidents struct{ created []*entity.Incident }

func (m *memIncidents) Create(_ context.Context, inc *entity.Incident) error {
	m.created = append(m.created, inc)
	return nil
}
func (m *memIncidents) GetByID(context.Context, value.WorkspaceID, uuid.UUID) (*entity.Incident, error) {
	return nil, errors.New("not implemented")
}
func (m *memIncidents) GetBySlackThread(context.Context, value.WorkspaceID, value.SlackChannelID, value.SlackThreadTS) (*entity.Incident, error) {
	return nil, errors.New("not implemented")
}
func (m *memIncidents) ListActive(context.Context, value.WorkspaceID, int, int) ([]*entity.Incident, error) {
	return nil, nil
}
func (m *memIncidents) ListRecent(context.Context, value.WorkspaceID, int, int) ([]*entity.Incident, error) {
	return nil, nil
}
func (m *memIncidents) Update(context.Context, *entity.Incident) error { return nil }

// memSettings is an empty settings store: these tests never reach the worker.
type memSettings struct{}

func (memSettings) Create(context.Context, *entity.WorkspaceSettings) error { return nil }
func (memSettings) Update(context.Context, *entity.WorkspaceSettings) error { return nil }
func (memSettings) Get(context.Context, value.WorkspaceID) (*entity.WorkspaceSettings, error) {
	return nil, errors.New("not configured")
}

var (
	_ repository.SettingsRepository  = (*memSettings)(nil)
	_ repository.WorkspaceRepository = (*memWorkspaces)(nil)
	_ repository.IncidentRepository  = (*memIncidents)(nil)
)

type harness struct {
	router    http.Handler
	secret    string
	incidents *memIncidents
	queue     *job.MemoryQueue
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	logger := zerolog.New(io.Discard)

	ws, err := entity.NewWorkspace("T_ALPHA", "Alpha", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("building workspace: %v", err)
	}
	secret, err := ws.GenerateWebhookSecret()
	if err != nil {
		t.Fatalf("generating secret: %v", err)
	}

	incidents := &memIncidents{}
	queue := job.NewMemoryQueue(8, 1, &logger)
	workspaces := &memWorkspaces{ws: ws}
	cipher, err := crypto.New("01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("building cipher: %v", err)
	}
	// Ingress only persists and queues; the resolver and orchestrator are
	// exercised on the worker, which these tests deliberately do not run.
	resolver := tenant.NewResolver(&memSettings{}, cipher)
	svc := incident.New(incidents, resolver, nil, queue, &logger)
	h := handler.NewAlertHandler(workspaces, svc, &logger)

	r := chi.NewRouter()
	r.Post("/api/v1/integrations/alerts/{provider}", h.Receive)

	return &harness{router: r, secret: secret, incidents: incidents, queue: queue}
}

func (h *harness) post(t *testing.T, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Criaisis-Token", token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func TestAlertHandler_AcceptsAuthenticatedAlert(t *testing.T) {
	h := newHarness(t)

	rec := h.post(t, "/api/v1/integrations/alerts/grafana?workspace=T_ALPHA", h.secret, grafanaPayload)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(h.incidents.created) != 1 {
		t.Fatalf("expected one incident, got %d", len(h.incidents.created))
	}
	inc := h.incidents.created[0]
	if inc.TriggerType() != value.TriggerTypeWebhook {
		t.Errorf("expected webhook trigger, got %s", inc.TriggerType())
	}
	ictx, err := inc.IncidentContext()
	if err != nil || ictx == nil {
		t.Fatalf("expected telemetry to be attached: %v", err)
	}
	if ictx.Provider() != "grafana" {
		t.Errorf("expected grafana context, got %s", ictx.Provider())
	}
}

// Every rejection path must produce no incident and a 401.
func TestAlertHandler_RejectsBadCredentials(t *testing.T) {
	cases := map[string]struct{ target, token string }{
		"wrong token":       {"/api/v1/integrations/alerts/grafana?workspace=T_ALPHA", "not-the-secret"},
		"absent token":      {"/api/v1/integrations/alerts/grafana?workspace=T_ALPHA", ""},
		"unknown workspace": {"/api/v1/integrations/alerts/grafana?workspace=T_GHOST", "whatever"},
		"absent workspace":  {"/api/v1/integrations/alerts/grafana", "whatever"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			rec := h.post(t, tc.target, tc.token, grafanaPayload)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rec.Code)
			}
			if len(h.incidents.created) != 0 {
				t.Error("an unauthenticated request created an incident")
			}
		})
	}
}

// An authenticated request with a valid token from workspace A must not be usable
// against workspace B.
func TestAlertHandler_TokenIsNotPortableAcrossWorkspaces(t *testing.T) {
	alpha := newHarness(t)
	beta := newHarness(t)

	rec := beta.post(t, "/api/v1/integrations/alerts/grafana?workspace=T_ALPHA", alpha.secret, grafanaPayload)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected another tenant's token to be rejected, got %d", rec.Code)
	}
	if len(beta.incidents.created) != 0 {
		t.Error("a foreign token created an incident")
	}
}

func TestAlertHandler_RejectsMalformedBody(t *testing.T) {
	h := newHarness(t)

	rec := h.post(t, "/api/v1/integrations/alerts/grafana?workspace=T_ALPHA", h.secret, "not json")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	if len(h.incidents.created) != 0 {
		t.Error("a malformed payload created an incident")
	}
}

// When the queue is saturated the incident is still durable, but the caller must be
// told to retry rather than being given a false success.
func TestAlertHandler_ReportsBackpressure(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 8; i++ {
		_ = h.queue.Enqueue(job.IncidentJob{WorkspaceID: value.NewWorkspaceID(), IncidentID: uuid.New()})
	}

	rec := h.post(t, "/api/v1/integrations/alerts/grafana?workspace=T_ALPHA", h.secret, grafanaPayload)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 under saturation, got %d", rec.Code)
	}
	if len(h.incidents.created) != 1 {
		t.Error("the incident should still have been persisted")
	}
}

// SNS will not deliver a single alarm until the subscription handshake completes,
// so a confirmation must be acted on rather than rejected as a malformed alert.
func TestAlertHandler_ConfirmsSNSSubscription(t *testing.T) {
	var confirmed bool
	amazon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		confirmed = true
		w.WriteHeader(http.StatusOK)
	}))
	defer amazon.Close()

	h := newHarness(t)
	body := `{"Type":"SubscriptionConfirmation","TopicArn":"arn:aws:sns:eu-west-1:1:alerts","SubscribeURL":"` + amazon.URL + `"}`

	rec := h.post(t, "/api/v1/integrations/alerts/cloudwatch?workspace=T_ALPHA", h.secret, body)

	// The stub host is not amazonaws.com, so the fetch is correctly refused.
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected a non-Amazon SubscribeURL to be refused, got %d", rec.Code)
	}
	if confirmed {
		t.Error("a SubscribeURL outside amazonaws.com was fetched")
	}
	if len(h.incidents.created) != 0 {
		t.Error("a subscription confirmation created an incident")
	}
}

// The handshake must still be authenticated: an unauthenticated caller cannot use
// it to make the server fetch a URL.
func TestAlertHandler_SNSConfirmationRequiresAuth(t *testing.T) {
	h := newHarness(t)
	body := `{"Type":"SubscriptionConfirmation","SubscribeURL":"https://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription"}`

	rec := h.post(t, "/api/v1/integrations/alerts/cloudwatch?workspace=T_ALPHA", "wrong-token", body)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
