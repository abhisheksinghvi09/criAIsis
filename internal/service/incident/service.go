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
	"criaisis/internal/job"
	"criaisis/internal/service/orchestrator"
	"criaisis/internal/service/tenant"

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

// Service owns incident creation, clash execution, and delivery of the verdict.
type Service struct {
	incidents repository.IncidentRepository
	resolver  *tenant.Resolver
	build     BuildOrchestrator
	queue     job.Queue
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

	if err := s.incidents.Create(ctx, inc); err != nil {
		return nil, fmt.Errorf("persisting incident: %w", err)
	}

	if err := s.queue.Enqueue(job.IncidentJob{WorkspaceID: inc.WorkspaceID(), IncidentID: inc.ID()}); err != nil {
		// The incident is durable; only the debate was shed. Surfacing this lets the
		// caller tell the user to retry rather than silently leaving a dead war room.
		return inc, fmt.Errorf("queueing clash: %w", err)
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
	ctx, cancel := context.WithTimeout(ctx, clashTimeout)
	defer cancel()

	inc, err := s.incidents.GetByID(ctx, j.WorkspaceID, j.IncidentID)
	if err != nil {
		return fmt.Errorf("loading incident %s: %w", j.IncidentID, err)
	}
	if inc.Status().IsResolved() {
		s.log.Info().Str("incident_id", inc.ID().String()).Msg("skipping clash: incident already resolved")
		return nil
	}

	// Resolve the tenant's own credentials before any model call. A workspace that
	// has not finished setup fails here with a clear reason, rather than part-way
	// through a debate.
	runtime, err := s.resolver.Resolve(ctx, inc.WorkspaceID())
	if err != nil {
		if errors.Is(err, tenant.ErrNotConfigured) {
			s.log.Warn().Err(err).Str("incident_id", inc.ID().String()).Msg("skipping clash: workspace setup incomplete")
			return nil
		}
		return fmt.Errorf("resolving workspace credentials: %w", err)
	}

	started := time.Now()
	result, err := s.build(runtime).RunClash(ctx, inc)
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

	return s.deliver(ctx, runtime, inc, result, time.Since(started))
}

// deliver posts the finished investigation to the tenant's channel.
//
// A delivery failure is logged but not returned: the transcript is already durable,
// and failing the job would re-run four model calls to fix a broken webhook URL.
func (s *Service) deliver(ctx context.Context, runtime *tenant.Runtime, inc *entity.Incident, result *orchestrator.ClashResult, elapsed time.Duration) error {
	if err := runtime.Notifier.Notify(ctx, buildReport(inc, result, elapsed)); err != nil {
		s.log.Error().Err(err).Str("incident_id", inc.ID().String()).Msg("could not deliver investigation")
		return nil
	}

	s.log.Info().Str("incident_id", inc.ID().String()).Msg("investigation delivered")
	return nil
}

// InvestigateNow runs an investigation synchronously and returns a summary.
//
// The queue is bypassed on purpose: this serves the setup check, where the
// customer is waiting on the response and needs the failure reason, not a job id.
func (s *Service) InvestigateNow(ctx context.Context, inc *entity.Incident) (*Summary, error) {
	if err := s.incidents.Create(ctx, inc); err != nil {
		return nil, fmt.Errorf("persisting incident: %w", err)
	}

	runtime, err := s.resolver.Resolve(ctx, inc.WorkspaceID())
	if err != nil {
		return nil, err
	}

	started := time.Now()
	result, err := s.build(runtime).RunClash(ctx, inc)
	if err != nil {
		return nil, fmt.Errorf("running investigation: %w", err)
	}
	elapsed := time.Since(started)

	report := buildReport(inc, result, elapsed)
	if err := runtime.Notifier.Notify(ctx, report); err != nil {
		return nil, fmt.Errorf("the investigation ran but delivery failed: %w", err)
	}

	return &Summary{
		Specialists:    result.SucceededCount(),
		Classification: result.Synthesis.Classification,
		OwningDomain:   result.Synthesis.OwningDomain,
		Citations:      report.Citations,
		Elapsed:        elapsed.Round(100 * time.Millisecond).String(),
	}, nil
}

// Summary is the caller-facing outcome of a synchronous investigation.
type Summary struct {
	Specialists    int
	Classification string
	OwningDomain   string
	Citations      []string
	Elapsed        string
}

// Resolve closes an incident and locks its transcript.
func (s *Service) Resolve(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Incident, error) {
	inc, err := s.incidents.GetByID(ctx, wsID, id)
	if err != nil {
		return nil, fmt.Errorf("loading incident: %w", err)
	}
	if err := inc.Resolve(time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := s.incidents.Update(ctx, inc); err != nil {
		return nil, fmt.Errorf("persisting resolution: %w", err)
	}
	return inc, nil
}
