package oncall_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"criaisis/internal/infrastructure/oncall"
)

func TestPagerDuty_ReturnsDedupedLowercasedEmails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oncalls" || r.URL.Query().Get("schedule_ids[]") != "S1" {
			t.Errorf("unexpected request %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Token token=k" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"oncalls":[{"user":{"email":"Ann@x.io"}},{"user":{"email":"ann@x.io"}},{"user":{"email":"bo@x.io"}}]}`))
	}))
	defer srv.Close()

	got, err := oncall.PagerDutyForTest(srv.URL, srv.Client()).OnCallEmails(context.Background())
	if err != nil || !reflect.DeepEqual(got, []string{"ann@x.io", "bo@x.io"}) {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestOpsgenie_ReadsFlatRecipients(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "GenieKey k" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":{"onCallRecipients":["cy@x.io"]}}`))
	}))
	defer srv.Close()

	got, err := oncall.OpsgenieForTest(srv.URL, srv.Client()).OnCallEmails(context.Background())
	if err != nil || !reflect.DeepEqual(got, []string{"cy@x.io"}) {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestNew_RequiresAScheduleAndKnownProvider(t *testing.T) {
	if _, err := oncall.New(oncall.ProviderPagerDuty, "k", " "); err == nil {
		t.Error("blank schedule accepted")
	}
	if _, err := oncall.ParseProvider("slack"); err == nil {
		t.Error("unknown provider accepted")
	}
}

func TestProvider_Non200IsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	if _, err := oncall.PagerDutyForTest(srv.URL, srv.Client()).OnCallEmails(context.Background()); err == nil {
		t.Error("expected an error on 401")
	}
}
