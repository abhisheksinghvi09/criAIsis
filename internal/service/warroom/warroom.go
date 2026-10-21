// Package warroom opens a private Slack channel per incident, invites whoever is
// on call, and posts the verdict there. Replies in that channel reach the same
// incident, so follow-up questions work without any thread bookkeeping.
package warroom

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/notify"
	"criaisis/internal/infrastructure/oncall"
	"criaisis/internal/infrastructure/slack"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ErrNotInstalled means the Slack app has not been installed for this workspace,
// so there is no bot token to act with.
var ErrNotInstalled = errors.New("slack app is not installed for this workspace")

// placeholderToken is what platform-created workspaces hold until Slack is installed.
const placeholderToken = "pending-slack-install"

// maxInvites bounds how many people one incident pulls into a channel.
const maxInvites = 25

// Room is an opened incident channel.
type Room struct {
	ChannelID string
	ThreadTS  string // the opening message; the verdict replies under it
	token     string
}

// Service creates and posts to incident channels.
type Service struct {
	workspaces repository.WorkspaceRepository
	cipher     *crypto.Cipher
	slack      slack.Client
	log        *zerolog.Logger
	// newLister is a seam so tests need no vendor HTTP server.
	newLister func(oncall.Provider, string, string) (oncall.Lister, error)
}

// New wires the war-room service.
func New(workspaces repository.WorkspaceRepository, cipher *crypto.Cipher, sl slack.Client, log *zerolog.Logger) *Service {
	return &Service{workspaces: workspaces, cipher: cipher, slack: sl, log: log, newLister: oncall.New}
}

// Open creates the channel, invites the incident's author and the current
// on-call, and posts the opening message. Invite failures are logged and skipped:
// a missing person must never stop the investigation from having a room.
func (s *Service) Open(ctx context.Context, inc *entity.Incident, settings *entity.WorkspaceSettings) (*Room, error) {
	token, err := s.botToken(ctx, inc)
	if err != nil {
		return nil, err
	}

	name := "inc-" + strings.ReplaceAll(inc.ID().String(), "-", "")[:8]
	channelID, err := s.slack.CreatePrivateChannel(ctx, token, name)
	if err != nil && strings.Contains(err.Error(), "name_taken") {
		// A crash after creating the channel but before recording it leaves the name
		// taken; a suffixed name keeps the retry moving.
		channelID, err = s.slack.CreatePrivateChannel(ctx, token, name+"-"+uuid.NewString()[:4])
	}
	if err != nil {
		return nil, fmt.Errorf("creating incident channel: %w", err)
	}

	for _, userID := range s.people(ctx, token, inc, settings) {
		if err := s.slack.InviteUser(ctx, token, channelID, userID); err != nil {
			s.log.Warn().Err(err).Str("user", userID).Str("channel", channelID).Msg("could not invite to incident channel")
		}
	}

	opening := fmt.Sprintf("*%s*\n%s · criAIsis is investigating now. The verdict will appear in this thread. "+
		"Mention me with a specialist name (network, database, application, security) to ask follow-ups.",
		notify.SlackEscape(inc.Title()), strings.ToUpper(inc.Severity().String()))
	ts, err := s.slack.PostPayload(ctx, token, map[string]any{"channel": channelID, "text": opening})
	if err != nil {
		return nil, fmt.Errorf("posting opening message: %w", err)
	}
	return &Room{ChannelID: channelID, ThreadTS: ts, token: token}, nil
}

// Resume rebuilds the room for an incident that already has one (a retried job).
func (s *Service) Resume(ctx context.Context, inc *entity.Incident) (*Room, error) {
	token, err := s.botToken(ctx, inc)
	if err != nil {
		return nil, err
	}
	return &Room{ChannelID: inc.SlackChannelID().String(), ThreadTS: inc.SlackThreadTS().String(), token: token}, nil
}

// PostVerdict posts the finished investigation into the room's thread.
func (s *Service) PostVerdict(ctx context.Context, room *Room, report notify.Report) error {
	payload := notify.SlackPayload(report)
	payload["channel"] = room.ChannelID
	payload["thread_ts"] = room.ThreadTS
	_, err := s.slack.PostPayload(ctx, room.token, payload)
	return err
}

// botToken decrypts the workspace's installed bot token.
func (s *Service) botToken(ctx context.Context, inc *entity.Incident) (string, error) {
	ws, err := s.workspaces.GetByID(ctx, inc.WorkspaceID())
	if err != nil || ws == nil {
		return "", fmt.Errorf("loading workspace: %w", err)
	}
	token, err := s.cipher.Decrypt(ws.SlackBotTokenEncrypted())
	if err != nil {
		return "", fmt.Errorf("decrypting bot token: %w", err)
	}
	if token == placeholderToken {
		return "", ErrNotInstalled
	}
	return token, nil
}

// people resolves the Slack user ids to invite: the incident's author when they
// are a real Slack user, then everyone currently on call.
func (s *Service) people(ctx context.Context, token string, inc *entity.Incident, settings *entity.WorkspaceSettings) []string {
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] && len(ids) < maxInvites {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	// Synthetic authors ("U_WEBHOOK", "U_SETUP_CHECK") contain an underscore; real ids do not.
	if author := inc.CreatedBySlackUser().String(); !strings.Contains(author, "_") {
		add(author)
	}

	provider, err := oncall.ParseProvider(settings.OnCallProvider())
	if err != nil || provider == oncall.ProviderNone {
		return ids
	}
	key, err := s.cipher.Decrypt(settings.OnCallAPIKeyEncrypted())
	if err != nil {
		s.log.Error().Err(err).Msg("decrypting on-call api key")
		return ids
	}
	lister, err := s.newLister(provider, key, settings.OnCallScheduleID())
	if err != nil {
		s.log.Error().Err(err).Msg("building on-call client")
		return ids
	}
	emails, err := lister.OnCallEmails(ctx)
	if err != nil {
		s.log.Error().Err(err).Str("provider", provider.String()).Msg("could not read the on-call schedule")
		return ids
	}
	for _, email := range emails {
		id, err := s.slack.LookupUserByEmail(ctx, token, email)
		if err != nil {
			s.log.Warn().Err(err).Msg("on-call person has no matching Slack account")
			continue
		}
		add(id)
	}
	return ids
}
