package warroom

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/oncall"
	"criaisis/internal/infrastructure/slack"

	"github.com/rs/zerolog"
)

type wsRepo struct {
	repository.WorkspaceRepository
	ws *entity.Workspace
}

func (r wsRepo) GetByID(context.Context, value.WorkspaceID) (*entity.Workspace, error) {
	return r.ws, nil
}

type fakeSlack struct {
	slack.Client
	channel string
	invited []string
	posted  []map[string]any
}

func (f *fakeSlack) CreatePrivateChannel(_ context.Context, _, name string) (string, error) {
	f.channel = name
	return "C_ROOM", nil
}
func (f *fakeSlack) InviteUser(_ context.Context, _, _, user string) error {
	f.invited = append(f.invited, user)
	return nil
}
func (f *fakeSlack) LookupUserByEmail(_ context.Context, _, email string) (string, error) {
	if email == "ghost@x.io" {
		return "", errors.New("users_not_found")
	}
	return "U_" + email[:3], nil
}
func (f *fakeSlack) PostPayload(_ context.Context, _ string, p map[string]any) (string, error) {
	f.posted = append(f.posted, p)
	return "1700.0001", nil
}

type fixedLister []string

func (l fixedLister) OnCallEmails(context.Context) ([]string, error) { return l, nil }

func setup(t *testing.T, botToken string) (*Service, *fakeSlack, *entity.Incident, *entity.WorkspaceSettings) {
	t.Helper()
	cipher, _ := crypto.New("01234567890123456789012345678901")
	sealedTok, _ := cipher.Encrypt(botToken)
	ws, err := entity.NewWorkspace("T1", "Team", sealedTok)
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := entity.NewWorkspaceSettings(ws.ID(), entity.SettingsDefaults{})
	key, _ := cipher.Encrypt("pd-key")
	if err := settings.SetOnCall("pagerduty", key, "SCHED"); err != nil {
		t.Fatal(err)
	}

	ch, _ := value.ParseSlackChannelID("C_UNROUTED")
	ts, _ := value.ParseSlackThreadTS("wh-1")
	author, _ := value.ParseSlackUserID("U_WEBHOOK")
	inc, err := entity.NewIncident(ws.ID(), "DB down", "d", ch, ts, value.SeveritySev1, author, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	sl := &fakeSlack{}
	svc := New(wsRepo{ws: ws}, cipher, sl, ptr(zerolog.Nop()))
	svc.newLister = func(oncall.Provider, string, string) (oncall.Lister, error) {
		return fixedLister{"ann@x.io", "ghost@x.io", "bob@x.io"}, nil
	}
	return svc, sl, inc, settings
}

func ptr[T any](v T) *T { return &v }

func TestOpen_CreatesChannelAndInvitesOnCallSkippingUnknowns(t *testing.T) {
	svc, sl, inc, settings := setup(t, "xoxb-real")

	room, err := svc.Open(context.Background(), inc, settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if room.ChannelID != "C_ROOM" || room.ThreadTS != "1700.0001" {
		t.Errorf("room = %+v", room)
	}
	if want := "inc-" + inc.ID().String()[:8]; sl.channel != want {
		t.Errorf("channel name = %q, want %q", sl.channel, want)
	}
	// The synthetic webhook author is not invited; the unmatched on-call is skipped.
	if want := []string{"U_ann", "U_bob"}; !reflect.DeepEqual(sl.invited, want) {
		t.Errorf("invited = %v, want %v", sl.invited, want)
	}
}

func TestOpen_RefusesAnUninstalledWorkspace(t *testing.T) {
	svc, _, inc, settings := setup(t, placeholderToken)
	if _, err := svc.Open(context.Background(), inc, settings); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("err = %v, want ErrNotInstalled", err)
	}
}
