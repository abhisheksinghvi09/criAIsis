package database

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"criaisis/internal/config"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog"
)

// migrationsFS embeds all SQL migration files directly into the compiled Go binary.
// This guarantees that migrations are always distributed with the application without separate artifact copying.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrator wraps the golang-migrate engine loaded from the embedded virtual filesystem.
type Migrator struct {
	m *migrate.Migrate
}

// NewMigrator initializes a new migrator instance backed by the embedded filesystem and database DSN.
func NewMigrator(dsn string) (*Migrator, error) {
	d, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("creating iofs migration driver: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, dsn)
	if err != nil {
		return nil, fmt.Errorf("constructing migrator: %w", err)
	}

	return &Migrator{m: m}, nil
}

// Close terminates both source and database driver connections.
func (m *Migrator) Close() error {
	srcErr, dbErr := m.m.Close()
	if srcErr != nil {
		return srcErr
	}
	return dbErr
}

// Up applies all outstanding up migrations. ErrNoChange is ignored as an expected idempotent state.
func (m *Migrator) Up() error {
	if err := m.m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations up: %w", err)
	}
	return nil
}

// Down rolls back all applied migrations. ErrNoChange is treated as a no-op.
func (m *Migrator) Down() error {
	if err := m.m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations down: %w", err)
	}
	return nil
}

// Step applies or rolls back n migration steps.
func (m *Migrator) Step(n int) error {
	if err := m.m.Steps(n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migration step (%d): %w", n, err)
	}
	return nil
}

// Version returns the active migration version and dirty flag.
func (m *Migrator) Version() (uint, bool, error) {
	return m.m.Version()
}

// Migrate is an automated helper designed for the application boot sequence.
// It applies all pending schema changes before the HTTP server begins serving incoming traffic.
func Migrate(ctx context.Context, logger *zerolog.Logger, cfg *config.Config) error {
	migrator, err := NewMigrator(cfg.Database.DSN())
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := migrator.Close(); closeErr != nil {
			logger.Warn().Err(closeErr).Msg("failed closing migration source/connection")
		}
	}()

	if err := migrator.Up(); err != nil {
		return err
	}

	version, dirty, err := migrator.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("retrieving migration version: %w", err)
	}

	logger.Info().
		Uint("version", version).
		Bool("dirty", dirty).
		Msg("database schema migrations applied successfully")

	return nil
}
