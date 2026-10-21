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
	"criaisis/internal/infrastructure/postgres"
	"criaisis/internal/infrastructure/slack"
	"criaisis/internal/router"
	"criaisis/internal/server"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/orchestrator"
	"criaisis/internal/service/sandbox"
	"criaisis/internal/service/tenant"
	"criaisis/internal/service/warroom"
	"criaisis/internal/telemetry"

	"github.com/rs/zerolog"
)

// version identifies this build in traces; set with -ldflags "-X main.version=...".
var version = "dev"

// shutdownTimeout gives in-flight requests and running debates time to finish.
const shutdownTimeout = 30 * time.Second

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply database migrations and exit")
	rotate := flag.Bool("rotate-credentials", false, "re-seal every stored credential under the current key, then exit")
	flag.Parse()

	logger := newLogger()

	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid configuration")
	}
	logger = logger.Level(logLevelFor(cfg.Primary.Env))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := telemetry.InitTracing(ctx, version)
	if err != nil {
		logger.Error().Err(err).Msg("tracing disabled: exporter setup failed")
	}
	defer func() { _ = shutdownTracing(context.Background()) }() // flush buffered spans

	if err := database.Migrate(ctx, &logger, cfg); err != nil {
		logger.Fatal().Err(err).Msg("database migration failed")
	}
	if *migrateOnly {
		logger.Info().Msg("migrations applied, exiting as requested")
		return
	}

	cipher, err := cfg.Security.Cipher()
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid credential encryption key")
	}

	srv, err := server.New(cfg, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("could not start server container")
	}

	if *rotate {
		n, err := postgres.RewrapCredentials(ctx, srv.DB.Pool, cipher)
		if err != nil {
			logger.Fatal().Err(err).Msg("credential rotation failed; nothing was changed")
		}
		logger.Info().Int("rewrapped", n).Msg("credentials re-sealed under the current key")
		_ = srv.DB.Close()
		return
	}

	repos := newRepositories(srv)
	if q, ok := srv.Queue.(interface{ Depth() int }); ok {
		telemetry.RegisterQueueDepth(q.Depth)
	}
	resolver := tenant.NewResolver(repos.settings, cipher)
	buildOrch := buildOrchestrator(repos, srv.Logger)
	slackAPI := slack.NewClient(nil)
	incidents := incident.New(repos.incidents, resolver, buildOrch, srv.Queue, srv.Logger).
		WithWarRoom(warroom.New(repos.workspaces, cipher, slackAPI, srv.Logger)).
		WithTx(repos.tx)

	// Recover investigations whose work was lost before the durable queue held it.
	if requeued, failed, err := repos.incidents.RecoverOrphaned(ctx, 24*time.Hour); err != nil {
		logger.Error().Err(err).Msg("could not recover orphaned incidents")
	} else if requeued+failed > 0 {
		logger.Warn().Int64("requeued", requeued).Int64("failed", failed).Msg("recovered orphaned incidents")
	}
	if pq, ok := srv.Queue.(*postgres.PostgresJobQueue); ok {
		pq.OnDead(incidents.MarkFailed)
		telemetry.RegisterDeadLetters(pq.DeadLetters)
	}

	// Workers run on their own context, not the signal one: cancelling on SIGTERM
	// would abort running debates and strand the queue instead of draining it.
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	srv.Queue.Start(workerCtx, incidents.HandleJob)

	// The Slack surface is only mounted once a signing secret exists to verify
	// requests against (BR0.1/NFR1). Without one, every /slack/* route is
	// absent rather than present-but-unverified: a missing secret must fail
	// closed, never fail open.
	var slackSig *handler.SlackSignatureMiddleware
	var slackInbound *handler.SlackInboundHandler
	if cfg.Slack.SigningSecret == "" {
		logger.Warn().Msg("CRIAISIS_SLACK_SIGNING_SECRET is not set; the Slack surface is disabled")
	} else {
		slackSig = handler.NewSlackSignatureMiddleware(cfg.Slack.SigningSecret, srv.Logger)
		slackInbound = handler.NewSlackInboundHandler(
			repos.workspaces, repos.incidents, incidents, resolver, buildOrch, cipher, slackAPI,
			handler.NewProvisioner(repos.workspaces, repos.settings, repos.personas, repos.tx, defaultsFrom(cfg)),
			cfg.Slack.ClientID, cfg.Slack.ClientSecret, srv.Logger,
		)
	}
	slackRate := handler.NewWorkspaceRateLimitMiddleware(30, time.Minute, srv.Logger)

	// Nil leaves the sandbox routes unmounted (router checks for it).
	var sandboxHandler *handler.SandboxHandler
	if cfg.Features.Sandbox {
		sandboxSvc := sandbox.NewService(repos.sandbox, repos.incidents, sandbox.NewExecProvisioner(""), srv.Logger)
		sandboxHandler = handler.NewSandboxHandler(sandboxSvc, srv.Logger)
	}

	srv.SetupHTTPServer(telemetry.HTTPHandler(router.New(srv, router.Handlers{
		Alert: handler.NewAlertHandler(repos.workspaces, incidents, &logger),
		Workspace: handler.NewWorkspaceHandler(
			repos.workspaces, repos.personas, repos.settings, repos.tx, resolver, incidents, cipher,
			defaultsFrom(cfg), &logger,
		).WithSandbox(cfg.Features.Sandbox),
		Runbook:        handler.NewRunbookHandler(repos.documents, repos.chunks, repos.personas, resolver, &logger),
		Incidents:      handler.NewIncidentReadHandler(repos.incidents, repos.turns, repos.personas, &logger),
		Personas:       handler.NewPersonaHandler(repos.personas, repos.documents, &logger),
		Health:         handler.NewHealthHandler(srv),
		Auth:           handler.NewAuth(repos.workspaces, cfg.Security.AdminAPIKey, &logger),
		SlackSignature: slackSig,
		SlackRateLimit: slackRate,
		SlackInbound:   slackInbound,
		Sandbox:        sandboxHandler,
	})))

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
	err = srv.Shutdown(shutdownCtx)
	cancelWorkers() // after the drain window: abort anything still running
	if err != nil {
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
	tx         *postgres.PostgresTxManager
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
		tx:         postgres.NewTxManager(pool),
	}
}

// buildOrchestrator binds the repositories once and leaves the model clients to be
// supplied per tenant, because every debate runs on that customer's own key.
func buildOrchestrator(repos repositories, log *zerolog.Logger) incident.BuildOrchestrator {
	return func(rt *tenant.Runtime) *orchestrator.Orchestrator {
		return orchestrator.New(repos.personas, repos.chunks, repos.turns, rt.Chat, rt.Embedder, log).WithTx(repos.tx)
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
