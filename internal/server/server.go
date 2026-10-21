// Package server holds the shared infrastructure container and the HTTP lifecycle.
// Repositories, services and handlers receive it rather than reaching for globals.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"criaisis/internal/config"
	"criaisis/internal/database"
	"criaisis/internal/infrastructure/postgres"
	"criaisis/internal/job"

	"github.com/rs/zerolog"
)

// clashWorkers caps concurrent debates. Each one fans out to four model calls, so
// this is the real ceiling on outbound model concurrency.
const clashWorkers = 4

// Server is the infrastructure container: configuration, logging, database pool and
// job queue, plus the HTTP server it owns.
type Server struct {
	Config *config.Config
	Logger *zerolog.Logger
	DB     *database.Database
	Queue  job.Queue

	httpServer *http.Server
}

// New builds the container and verifies the database is reachable.
func New(cfg *config.Config, logger *zerolog.Logger) (*Server, error) {
	db, err := database.New(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("initializing database: %w", err)
	}

	return &Server{
		Config: cfg,
		Logger: logger,
		DB:     db,
		Queue: postgres.NewJobQueue(db.Pool, postgres.JobQueueConfig{
			Workers: clashWorkers,
			// A clash is bounded at 90s; the lease outlives it with margin and is
			// renewed every third of its length while the job runs.
			Lease:          2 * time.Minute,
			MaxAttempts:    4,
			PerTenantLimit: 2,
		}, logger),
	}, nil
}

// SetupHTTPServer attaches the router and applies the configured timeouts.
func (s *Server) SetupHTTPServer(handler http.Handler) {
	s.httpServer = &http.Server{
		Addr:         net.JoinHostPort("", s.Config.Server.Port),
		Handler:      handler,
		ReadTimeout:  time.Duration(s.Config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.Config.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(s.Config.Server.IdleTimeout) * time.Second,
	}
}

// Start serves until the listener closes. ErrServerClosed is a normal shutdown.
func (s *Server) Start() error {
	if s.httpServer == nil {
		return errors.New("http server not configured: call SetupHTTPServer first")
	}

	s.Logger.Info().
		Str("port", s.Config.Server.Port).
		Str("env", s.Config.Primary.Env).
		Msg("criAIsis listening")

	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// Shutdown closes resources in reverse order of construction: stop accepting
// requests, drain in-flight debates, then release the database pool.
func (s *Server) Shutdown(ctx context.Context) error {
	var errs []error

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http shutdown: %w", err))
		}
	}
	if s.Queue != nil {
		if err := s.Queue.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("queue shutdown: %w", err))
		}
	}
	if s.DB != nil {
		if err := s.DB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("database close: %w", err))
		}
	}

	return errors.Join(errs...)
}
