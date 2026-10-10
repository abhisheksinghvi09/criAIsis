package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var (
	ErrIncidentNotResolved      = errors.New("incident is not resolved")
	ErrActiveReproductionExists = errors.New("an active reproduction is already in progress or ready for this incident")
	ErrSandboxNotReady          = errors.New("sandbox reproduction is not ready")
	ErrInvalidScenario          = errors.New("invalid scenario selected")
)

var ValidScenarios = []string{
	"oom_crashloop",
	"db_connection_exhaustion",
	"checkout_packet_loss",
}

// Provisioner defines the contract for container lifecycle management.
type Provisioner interface {
	Provision(ctx context.Context, scenarioID string) (containerRef string, err error)
}

// ExecProvisioner executes scripts/sandbox_reproduce.sh.
type ExecProvisioner struct {
	scriptPath string
}

func NewExecProvisioner(scriptPath string) *ExecProvisioner {
	if scriptPath == "" {
		scriptPath = "scripts/sandbox_reproduce.sh"
	}
	return &ExecProvisioner{scriptPath: scriptPath}
}

func (p *ExecProvisioner) Provision(ctx context.Context, scenarioID string) (string, error) {
	cmd := exec.CommandContext(ctx, p.scriptPath, scenarioID, "--keep")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("provisioning failed: %w (output: %s)", err, string(out))
	}
	containerRef := fmt.Sprintf("sandbox-%s-%d", scenarioID, time.Now().Unix())
	return containerRef, nil
}

// AccessDetails contains the connection and logging endpoints for a ready sandbox.
type AccessDetails struct {
	ContainerRef string `json:"container_ref"`
	AccessURL    string `json:"access_url"`
	LogsURL      string `json:"logs_url"`
}

// Service coordinates sandbox reproduction workflows.
type Service struct {
	reproductions repository.SandboxReproductionRepository
	incidents     repository.IncidentRepository
	provisioner   Provisioner
	log           *zerolog.Logger
}

// NewService constructs a SandboxService.
func NewService(
	reproductions repository.SandboxReproductionRepository,
	incidents repository.IncidentRepository,
	provisioner Provisioner,
	log *zerolog.Logger,
) *Service {
	return &Service{
		reproductions: reproductions,
		incidents:     incidents,
		provisioner:   provisioner,
		log:           log,
	}
}

// Trigger starts a reproduction attempt for a resolved incident.
func (s *Service) Trigger(ctx context.Context, wsID value.WorkspaceID, incidentID uuid.UUID) (*entity.SandboxReproduction, error) {
	inc, err := s.incidents.GetByID(ctx, wsID, incidentID)
	if err != nil {
		return nil, fmt.Errorf("loading incident: %w", err)
	}

	// BR5.2: Incident must be resolved
	if !inc.Status().IsResolved() {
		return nil, ErrIncidentNotResolved
	}

	// BR5.1: Concurrency check - only one active reproduction
	active, err := s.reproductions.GetActiveForIncident(ctx, wsID, incidentID)
	if err != nil {
		return nil, fmt.Errorf("checking active reproduction: %w", err)
	}
	if active != nil {
		return nil, ErrActiveReproductionExists
	}

	sr, err := entity.NewSandboxReproduction(incidentID, wsID)
	if err != nil {
		return nil, fmt.Errorf("creating reproduction entity: %w", err)
	}

	// Score scenario from IncidentContext
	incCtx, _ := inc.IncidentContext()
	scenarioID := s.scoreScenario(incCtx)
	if scenarioID != "" {
		_ = sr.SetScenarioID(scenarioID)
	} else {
		// BR5.3: No match above threshold -> UnmatchedFallback
		sr.SetUnmatchedFallback()
	}

	if err := s.reproductions.Create(ctx, sr); err != nil {
		return nil, fmt.Errorf("saving reproduction: %w", err)
	}

	if sr.Status() == value.SandboxStatusProvisioning {
		s.startProvisioning(wsID, sr.ID(), scenarioID)
	}

	return sr, nil
}

// SelectScenario restarts provisioning for an UnmatchedFallback reproduction.
func (s *Service) SelectScenario(ctx context.Context, wsID value.WorkspaceID, reprID uuid.UUID, scenarioID string) (*entity.SandboxReproduction, error) {
	if !isValidScenario(scenarioID) {
		return nil, ErrInvalidScenario
	}

	sr, err := s.reproductions.GetByID(ctx, wsID, reprID)
	if err != nil {
		return nil, fmt.Errorf("loading reproduction: %w", err)
	}

	if err := sr.SelectScenario(scenarioID); err != nil {
		return nil, err
	}

	if err := s.reproductions.Update(ctx, sr); err != nil {
		return nil, fmt.Errorf("updating reproduction: %w", err)
	}

	s.startProvisioning(wsID, sr.ID(), scenarioID)
	return sr, nil
}

// Get retrieves current reproduction status.
func (s *Service) Get(ctx context.Context, wsID value.WorkspaceID, reprID uuid.UUID) (*entity.SandboxReproduction, error) {
	return s.reproductions.GetByID(ctx, wsID, reprID)
}

// GetAccess returns connection and log URLs for a ready sandbox (BR5.6).
func (s *Service) GetAccess(ctx context.Context, wsID value.WorkspaceID, reprID uuid.UUID) (*AccessDetails, error) {
	sr, err := s.Get(ctx, wsID, reprID)
	if err != nil {
		return nil, err
	}

	if sr.Status() != value.SandboxStatusReady {
		return nil, ErrSandboxNotReady
	}

	ref := sr.ContainerRef()
	return &AccessDetails{
		ContainerRef: ref,
		AccessURL:    fmt.Sprintf("http://localhost:8080/sandbox/%s", ref),
		LogsURL:      fmt.Sprintf("http://localhost:8080/sandbox/%s/logs", ref),
	}, nil
}

func (s *Service) startProvisioning(wsID value.WorkspaceID, reprID uuid.UUID, scenarioID string) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error().
					Str("reproduction_id", reprID.String()).
					Interface("panic", rec).
					Msg("panic in sandbox provisioning background worker (NFR4)")
			}
		}()

		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		ref, err := s.provisioner.Provision(bgCtx, scenarioID)
		sr, loadErr := s.reproductions.GetByID(bgCtx, wsID, reprID)
		if loadErr != nil || sr == nil {
			s.log.Error().Err(loadErr).Str("reproduction_id", reprID.String()).Msg("failed loading reproduction after provisioning")
			return
		}

		if err != nil {
			s.log.Error().Err(err).
				Str("reproduction_id", reprID.String()).
				Str("scenario_id", scenarioID).
				Msg("sandbox provisioning failed (NFR-REL.3)")
			sr.MarkFailed()
		} else {
			s.log.Info().
				Str("reproduction_id", reprID.String()).
				Str("container_ref", ref).
				Msg("sandbox provisioning ready")
			sr.MarkReady(ref)
		}

		if updateErr := s.reproductions.Update(bgCtx, sr); updateErr != nil {
			s.log.Error().Err(updateErr).
				Str("reproduction_id", reprID.String()).
				Str("status", sr.Status().String()).
				Msg("failed persisting sandbox provisioning result")
		}
	}()
}

// scoreScenario matches an incident's context against the 3 scenario archetypes.
func (s *Service) scoreScenario(ctx *value.IncidentContext) string {
	if ctx == nil {
		return ""
	}

	// Token search in logs and stack traces
	var combinedText string
	for _, l := range ctx.ErrorLogs() {
		combinedText += strings.ToLower(l) + " "
	}
	for _, st := range ctx.StackTraces() {
		combinedText += strings.ToLower(st) + " "
	}

	scores := map[string]int{
		"oom_crashloop":            0,
		"db_connection_exhaustion": 0,
		"checkout_packet_loss":     0,
	}

	// OOM Crashloop markers
	if strings.Contains(combinedText, "oom") || strings.Contains(combinedText, "memory") || strings.Contains(combinedText, "137") {
		scores["oom_crashloop"] += 2
	}
	if strings.Contains(combinedText, "crashloop") || strings.Contains(combinedText, "makeslice") {
		scores["oom_crashloop"] += 2
	}

	// DB Connection Exhaustion markers
	if strings.Contains(combinedText, "connection") || strings.Contains(combinedText, "pg_stat_activity") || strings.Contains(combinedText, "max_connections") {
		scores["db_connection_exhaustion"] += 2
	}
	if strings.Contains(combinedText, "pool") || strings.Contains(combinedText, "idle in transaction") {
		scores["db_connection_exhaustion"] += 2
	}

	// Packet Loss markers
	if strings.Contains(combinedText, "packet") || strings.Contains(combinedText, "loss") || strings.Contains(combinedText, "tc qdisc") || strings.Contains(combinedText, "timeout") {
		scores["checkout_packet_loss"] += 2
	}

	bestScenario := ""
	bestScore := 0
	for sc, score := range scores {
		if score > bestScore {
			bestScore = score
			bestScenario = sc
		}
	}

	if bestScore >= 2 {
		return bestScenario
	}
	return ""
}

func isValidScenario(id string) bool {
	for _, s := range ValidScenarios {
		if s == id {
			return true
		}
	}
	return false
}
