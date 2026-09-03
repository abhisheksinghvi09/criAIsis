package database

import (
	"context"
	"fmt"
	"time"

	"criaisis/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// DatabasePingTimeout defines the maximum deadline allowed for the initial database health probe.
const DatabasePingTimeout = 10 * time.Second

// Database wraps a thread-safe pgx connection pool and structured logger.
// It centralizes connection lifecycle management and prevents unpooled connection leaks.
type Database struct {
	Pool *pgxpool.Pool
	log  *zerolog.Logger
}

// New initializes and validates a new PostgreSQL connection pool based on validated configuration.
// It executes an explicit ping to verify network reachability and credentials before returning.
func New(cfg *config.Config, logger *zerolog.Logger) (*Database, error) {
	pgxPoolConfig, err := pgxpool.ParseConfig(cfg.Database.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgx pool config: %w", err)
	}

	// Apply connection pool sizing and connection lifetime limits
	if cfg.Database.MaxOpenConns > 0 {
		pgxPoolConfig.MaxConns = int32(cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns > 0 {
		pgxPoolConfig.MinConns = int32(cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		pgxPoolConfig.MaxConnLifetime = time.Duration(cfg.Database.ConnMaxLifetime) * time.Second
	}
	if cfg.Database.ConnMaxIdleTime > 0 {
		pgxPoolConfig.MaxConnIdleTime = time.Duration(cfg.Database.ConnMaxIdleTime) * time.Second
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), pgxPoolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgx pool: %w", err)
	}

	// Verify database connection health with a hard timeout before marking ready
	ctx, cancel := context.WithTimeout(context.Background(), DatabasePingTimeout)
	defer cancel()

	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info().
		Str("host", cfg.Database.Host).
		Int("port", cfg.Database.Port).
		Str("name", cfg.Database.Name).
		Msg("connected to PostgreSQL database")

	return &Database{
		Pool: pool,
		log:  logger,
	}, nil
}

// Close gracefully drains and terminates all active connections in the pool.
func (db *Database) Close() error {
	db.log.Info().Msg("closing database connection pool")
	db.Pool.Close()
	return nil
}
