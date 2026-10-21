// Package incident coordinates the lifecycle of a war room: creating it from an
// ingress event, running the clash on a worker, and resolving it.
package incident

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/notify"
	"criaisis/internal/job"
	"criaisis/internal/service/orchestrator"
	"criaisis/internal/service/tenant"
	"criaisis/internal/service/warroom"
	"criaisis/internal/telemetry"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// clashTimeout bounds one end-to-end investigation on the worker. The product
// targets 25s; this is the hard stop for a run that has gone wrong.
const clashTimeout = 90 * time.Second

// BuildOrchestrator constructs a clash engine bound to one tenant's model clients.
// The composition root supplies it with the repositories already bound, so this
// service does not need to know how the engine is assembled.
type BuildOrchestrator func(*tenant.Runtime) *orchestrator.Orchestrator

// WarRoom opens a per-incident Slack channel and posts the verdict into it.
type WarRoom interface {
	Open(ctx context.Context, inc *entity.Incident, settings *entity.WorkspaceSettings) (*warroom.Room, error)
	Resume(ctx context.Context, inc *entity.Incident) (*warroom.Room, error)
	PostVerdict(ctx context.Context, room *warroom.Room, report notify.Report) error
}

// Service owns incident creation, clash execution, and delivery of the verdict.
type Service struct {
	incidents repository.IncidentRepository
	resolver  *tenant.Resolver
	build     BuildOrchestrator
	queue     job.Queue
	warRoom   WarRoom
	tx        repository.TransactionManager
	log       *zerolog.Logger
}

// New wires the incident service.
func New(
	incidents repository.IncidentRepository,
	resolver *tenant.Resolver,
	build BuildOrchestrator,
	queue job.Queue,
	log *zerolog.Logger,
) *Service {
	return &Service{incidents: incidents, resolver: resolver, build: build, queue: queue, log: log}
}

// WithWarRoom enables per-incident Slack channels for tenants that choose them.
func (s *Service) WithWarRoom(w WarRoom) *Service {
	s.warRoom = w
	return s
}

// WithTx makes Open atomic: the incident and its job commit together, so there is
// never an incident without work queued for it, nor a job for a missing incident.
func (s *Service) WithTx(tx repository.TransactionManager) *Service {
	s.tx = tx
	return s
}

// NewIncidentInput describes an incident to open, from either ingress path.
type NewIncidentInput struct {
	WorkspaceID value.WorkspaceID
	Title       string
	Description string
	Severity    value.Severity
	Trigger     value.TriggerType
	ChannelID   value.SlackChannelID
	ThreadTS    value.SlackThreadTS
	CreatedBy   value.SlackUserID
	Context     *value.IncidentContext
}

// Open persists an incident and queues the clash. It deliberately returns as soon
// as the record is durable, so ingress can acknowledge inside Slack's timeout.
func (s *Service) Open(ctx context.Context, in NewIncidentInput) (*entity.Incident, error) {
	inc, err := entity.NewIncident(
		in.WorkspaceID, in.Title, in.Description,
		in.ChannelID, in.ThreadTS, in.Severity, in.CreatedBy,
		json.RawMessage("{}"),
	)
	if err != nil {
		return nil, fmt.Errorf("building incident: %w", err)
	}
	if err := inc.SetTriggerType(in.Trigger); err != nil {
		return nil, fmt.Errorf("setting trigger type: %w", err)
	}
	if in.Context != nil {
		if err := inc.SetIncidentContext(*in.Context); err != nil {
			return nil, fmt.Errorf("attaching incident context: %w", err)
		}
	}

	persistAndQueue := func(ctx context.Context) error {
		if err := s.incidents.Create(ctx, inc); err != nil {
			return fmt.Errorf("persisting incident: %w", err)
		}
		if err := s.queue.Enqueue(ctx, job.IncidentJob{WorkspaceID: inc.WorkspaceID(), IncidentID: inc.ID()}); err != nil {
			return fmt.Errorf("queueing clash: %w", err)
		}
		return nil
	}

	if s.tx != nil {
		if err := s.tx.WithinTransaction(ctx, persistAndQueue); err != nil {
			return nil, err
		}
	} else if err := persistAndQueue(ctx); err != nil {
		// Without a transaction the incident may be durable while only the debate was
		// shed; returning it lets the caller report which incident to retry.
		return inc, err
	}

	s.log.Info().
		Str("incident_id", inc.ID().String()).
		Str("severity", inc.Severity().String()).
		Str("trigger", inc.TriggerType().String()).
		Msg("incident opened")

	return inc, nil
}

// HandleJob is the worker entry point: reload the incident and run the clash.
// State is reloaded rather than carried on the job so a debate never runs against
// an incident that was resolved while it sat in the queue.
func (s *Service) HandleJob(ctx context.Context, j job.IncidentJob) error {
	err := s.handleJob(ctx, j)
	switch {
	case err == nil:
		return nil
	case job.IsPermanent(err) || j.FinalAttempt():
		telemetry.ObserveInvestigation("failed", 0)
		s.MarkFailed(ctx, j)
	default:
		// The queue will retry; the incident stays investigating until it succeeds
		// or runs out of attempts.
		telemetry.ObserveInvestigation("retry", 0)
	}
	return err
}

// MarkFailed records that the investigation did not complete, so the incident
// does not sit in "investigating" forever. It runs detached from ctx because the
// failure is often the ctx itself expiring or being cancelled.
func (s *Service) MarkFailed(ctx context.Context, j job.IncidentJob) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	_, err := s.mutate(ctx, j.WorkspaceID, j.IncidentID, func(inc *entity.Incident) error {
		return inc.Fail(time.Now().UTC()) // refuses if a human resolved it meanwhile
	})
	if err != nil && !errors.Is(err, entity.ErrNotInvestigating) {
		s.log.Error().Err(err).Str("incident_id", j.IncidentID.String()).Msg("could not mark incident failed")
	}
}

func (s *Service) handleJob(ctx context.Context, j job.IncidentJob) error {
	ctx, cancel := context.WithTimeout(ctx, clashTimeout)
	defer cancel()

	inc, err := s.incidents.GetByID(ctx, j.WorkspaceID, j.IncidentID)
	if err != nil {
		return fmt.Errorf("loading incident %s: %w", j.IncidentID, err)
	}
	if inc.Status().IsResolved() {
		s.log.Info().Str("incident_id", inc.ID().String()).Msg("skipping clash: incident already resolved")
		telemetry.ObserveInvestigation("skipped", 0)
		return nil
	}

	// Resolve the tenant's own credentials before any model call. A workspace that
	// has not finished setup fails here with a clear reason, rather than part-way
	// through a debate.
	runtime, err := s.resolver.Resolve(ctx, inc.WorkspaceID())
	if err != nil {
		if errors.Is(err, tenant.ErrNotConfigured) {
			s.log.Warn().Err(err).Str("incident_id", inc.ID().String()).Msg("skipping clash: workspace setup incomplete")
			// Permanent: retrying cannot help until the tenant finishes setup.
			return job.Permanent(err)
		}
		return fmt.Errorf("resolving workspace credentials: %w", err)
	}

	room := s.openRoom(ctx, runtime, inc)

	started := time.Now()
	result, err := s.build(runtime).RunClash(ctx, inc)
	if errors.Is(err, orchestrator.ErrAlreadyInvestigated) {
		// A retry after the transcript committed but before the job was acknowledged.
		s.log.Info().Str("incident_id", inc.ID().String()).Msg("skipping clash: transcript already complete")
		telemetry.ObserveInvestigation("skipped", 0)
		return nil
	}
	if err != nil {
		return fmt.Errorf("running clash for %s: %w", inc.ID(), err)
	}

	s.log.Info().
		Str("incident_id", inc.ID().String()).
		Int("specialists", result.SucceededCount()).
		Str("classification", result.Synthesis.Classification).
		Str("owning_domain", result.Synthesis.OwningDomain).
		Dur("elapsed", time.Since(started)).
		Msg("clash complete")

	telemetry.ObserveInvestigation("ok", time.Since(started))
	return s.deliver(ctx, runtime, room, inc, result, time.Since(started))
}

// openRoom creates the incident's Slack channel when the tenant uses them and
// points the incident at it, so replies there reach this incident. A failure is
// logged and returns nil: the transcript is still durable in the dashboard.
func (s *Service) openRoom(ctx context.Context, runtime *tenant.Runtime, inc *entity.Incident) *warroom.Room {
	if s.warRoom == nil || !runtime.Settings.UsesIncidentChannels() {
		return nil
	}
	if inc.HasWarRoom() {
		room, err := s.warRoom.Resume(ctx, inc)
		if err != nil {
			s.log.Error().Err(err).Str("incident_id", inc.ID().String()).Msg("could not resume incident channel")
			return nil
		}
		return room
	}
	room, err := s.warRoom.Open(ctx, inc, runtime.Settings)
	if err != nil {
		s.log.Error().Err(err).Str("incident_id", inc.ID().String()).Msg("could not open incident channel")
		return nil
	}
	moved, err := s.mutate(ctx, inc.WorkspaceID(), inc.ID(), func(fresh *entity.Incident) error {
		return fresh.MoveToSlackThread(value.SlackChannelID(room.ChannelID), value.SlackThreadTS(room.ThreadTS))
	})
	if err != nil {
		s.log.Error().Err(err).Str("incident_id", inc.ID().String()).Msg("could not record incident channel")
	} else {
		*inc = *moved // the rest of this job works from the saved state
	}
	return room
}

// deliver posts the finished investigation to the tenant's channel.
//
// A delivery failure is logged but not returned: the transcript is already durable,
// and failing the job would re-run four model calls to fix a broken webhook URL.
func (s *Service) deliver(ctx context.Context, runtime *tenant.Runtime, room *warroom.Room, inc *entity.Incident, result *orchestrator.ClashResult, elapsed time.Duration) error {
	report := buildReport(inc, result, elapsed)
	var err error
	if room != nil {
		err = s.warRoom.PostVerdict(ctx, room, report)
	} else {
		err = runtime.Notifier.Notify(ctx, report)
	}
	if err != nil {
		s.log.Error().Err(err).Str("incident_id", inc.ID().String()).Msg("could not deliver investigation")
		return nil
	}

	s.log.Info().Str("incident_id", inc.ID().String()).Msg("investigation delivered")
	return nil
}

// FindByThread returns the incident already stored under a Slack channel and
// thread key. Any lookup failure reads as "not found": the callers use this only
// to avoid creating a duplicate, and the unique index is the real guard.
func (s *Service) FindByThread(ctx context.Context, wsID value.WorkspaceID, ch value.SlackChannelID, ts value.SlackThreadTS) (*entity.Incident, bool) {
	inc, err := s.incidents.GetBySlackThread(ctx, wsID, ch, ts)
	return inc, err == nil && inc != nil
}

// Resolve closes an incident and locks its transcript.
func (s *Service) Resolve(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Incident, error) {
	return s.mutate(ctx, wsID, id, func(inc *entity.Incident) error {
		return inc.Resolve(time.Now().UTC())
	})
}

// mutate applies one change to a fresh copy of an incident and saves it, retrying
// if a concurrent writer (a worker failing the job, a human resolving it) got
// there first. The change sees the latest state each time, so it can refuse an
// incident that is no longer in the state it expects.
func (s *Service) mutate(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID, change func(*entity.Incident) error) (*entity.Incident, error) {
	var inc *entity.Incident
	err := repository.RetryOnConflict(ctx, func() error {
		var err error
		if inc, err = s.incidents.GetByID(ctx, wsID, id); err != nil {
			return fmt.Errorf("loading incident: %w", err)
		}
		if err := change(inc); err != nil {
			return err
		}
		return s.incidents.Update(ctx, inc)
	})
	if err != nil {
		return nil, err
	}
	return inc, nil
}
