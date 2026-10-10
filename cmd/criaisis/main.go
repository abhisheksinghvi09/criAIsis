// Command criaisis is the composition root: it loads configuration, runs
// migrations, wires the dependency chain, and manages process lifecycle.
// It contains no business logic and no configuration literals.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"criaisis/internal/config"
	"criaisis/internal/database"
	"criaisis/internal/domain/entity"
	"criaisis/internal/handler"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/infrastructure/postgres"
	"criaisis/internal/infrastructure/slack"
	"criaisis/internal/infrastructure/telemetry"
	"criaisis/internal/router"
	"criaisis/internal/server"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/orchestrator"
	"criaisis/internal/service/sandbox"
	"criaisis/internal/service/tenant"

	"github.com/rs/zerolog"
)

// shutdownTimeout gives in-flight requests and running debates time to finish.
const shutdownTimeout = 30 * time.Second

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply database migrations and exit")
	flag.Parse()

	logger := newLogger()

	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid configuration")
	}
	logger = logger.Level(logLevelFor(cfg.Primary.Env))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := database.Migrate(ctx, &logger, cfg); err != nil {
		logger.Fatal().Err(err).Msg("database migration failed")
	}
	if *migrateOnly {
		logger.Info().Msg("migrations applied, exiting as requested")
		return
	}

	cipher, err := crypto.New(cfg.Security.CredentialEncryptionKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid credential encryption key")
	}

	srv, err := server.New(cfg, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("could not start server container")
	}

	repos := newRepositories(srv)
	resolver := tenant.NewResolver(repos.settings, cipher)
	buildOrch := buildOrchestrator(repos, srv.Logger)
	incidents := incident.New(repos.incidents, resolver, buildOrch, srv.Queue, srv.Logger)

	srv.Queue.Start(ctx, incidents.HandleJob)

	// The Slack surface is only mounted once a signing secret exists to verify
	// requests against (BR0.1/NFR1). Without one, every /slack/* route is
	// absent rather than present-but-unverified: a missing secret must fail
	// closed, never fail open.
	var slackSig *handler.SlackSignatureMiddleware
	var slackInbound *handler.SlackInboundHandler
	if cfg.Slack.SigningSecret == "" {
		logger.Warn().Msg("CRIAISIS_SLACK_SIGNING_SECRET is not set; the Slack surface is disabled")
	} else {
		slackClient := slack.NewClient(nil)
		slackSig = handler.NewSlackSignatureMiddleware(cfg.Slack.SigningSecret, srv.Logger)
		slackInbound = handler.NewSlackInboundHandler(
			repos.workspaces, repos.incidents, incidents, resolver, buildOrch, cipher, slackClient,
			cfg.Slack.ClientID, cfg.Slack.ClientSecret, srv.Logger,
		)
	}
	slackRate := handler.NewWorkspaceRateLimitMiddleware(30, time.Minute, srv.Logger)

	sandboxSvc := sandbox.NewService(repos.sandbox, repos.incidents, sandbox.NewExecProvisioner(""), srv.Logger)
	sandboxHandler := handler.NewSandboxHandler(sandboxSvc, srv.Logger)

	srv.SetupHTTPServer(router.New(srv, router.Handlers{
		Alert: handler.NewAlertHandler(repos.workspaces, incidents, &logger),
		Workspace: handler.NewWorkspaceHandler(
			repos.workspaces, repos.personas, repos.settings, resolver, incidents, cipher,
			defaultsFrom(cfg), &logger,
		),
		Runbook:        handler.NewRunbookHandler(repos.documents, repos.chunks, repos.personas, resolver, &logger),
		Incidents:      handler.NewIncidentReadHandler(repos.incidents, repos.turns, repos.personas, &logger),
		Personas:       handler.NewPersonaHandler(repos.personas, repos.documents, &logger),
		Health:         handler.NewHealthHandler(srv),
		Auth:           handler.NewAuth(repos.workspaces, cfg.Security.AdminAPIKey, &logger),
		SlackSignature: slackSig,
		SlackRateLimit: slackRate,
		SlackInbound:   slackInbound,
		Sandbox:        sandboxHandler,
	}))

	go func() {
		if err := srv.Start(); err != nil {
			logger.Error().Err(err).Msg("http server stopped")
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info().Msg("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("unclean shutdown")
		os.Exit(1)
	}
	logger.Info().Msg("shutdown complete")
}

// repositories groups the persistence adapters.
type repositories struct {
	workspaces *postgres.PostgresWorkspaceRepository
	settings   *postgres.PostgresSettingsRepository
	personas   *postgres.PostgresPersonaRepository
	documents  *postgres.PostgresDocumentRepository
	chunks     *postgres.PostgresChunkRepository
	incidents  *postgres.PostgresIncidentRepository
	turns      *postgres.PostgresDebateTurnRepository
	sandbox    *postgres.PostgresSandboxReproductionRepository
}

// newRepositories constructs every repository against the shared pool.
func newRepositories(srv *server.Server) repositories {
	pool := srv.DB.Pool
	return repositories{
		workspaces: postgres.NewWorkspaceRepository(pool),
		settings:   postgres.NewSettingsRepository(pool),
		personas:   postgres.NewPersonaRepository(pool),
		documents:  postgres.NewDocumentRepository(pool),
		chunks:     postgres.NewChunkRepository(pool),
		incidents:  postgres.NewIncidentRepository(pool),
		turns:      postgres.NewDebateTurnRepository(pool),
		sandbox:    postgres.NewSandboxReproductionRepository(pool),
	}
}

// buildOrchestrator binds the repositories once and leaves the model clients to be
// supplied per tenant, because every debate runs on that customer's own key.
func buildOrchestrator(repos repositories, log *zerolog.Logger) incident.BuildOrchestrator {
	tools := telemetry.NewRegistry()
	return func(rt *tenant.Runtime) *orchestrator.Orchestrator {
		return orchestrator.New(repos.personas, repos.chunks, repos.turns, rt.Chat, rt.Embedder, tools, log)
	}
}

// defaultsFrom carries platform model defaults to newly created tenants. It never
// carries a key: those are the tenant's to supply.
func defaultsFrom(cfg *config.Config) entity.SettingsDefaults {
	return entity.SettingsDefaults{
		SpecialistModel:  cfg.LLM.SpecialistModel,
		SynthesisModel:   cfg.LLM.SynthesisModel,
		EmbeddingModel:   cfg.LLM.EmbeddingModel,
		EmbeddingBaseURL: cfg.LLM.EmbeddingBaseURL,
	}
}

// newLogger configures structured logging.
func newLogger() zerolog.Logger {
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}

// logLevelFor keeps production quiet and local development verbose.
func logLevelFor(env string) zerolog.Level {
	if env == "production" {
		return zerolog.InfoLevel
	}
	return zerolog.DebugLevel
}
