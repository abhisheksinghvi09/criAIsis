package postgres

import (
	"context"
	"fmt"

	"criaisis/internal/domain/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresTxManager coordinates atomic multi-entity mutations using pgx transactions.
type PostgresTxManager struct {
	pool *pgxpool.Pool
}

var _ repository.TransactionManager = (*PostgresTxManager)(nil)

// NewTxManager constructs a transaction coordinator backed by the connection pool.
func NewTxManager(pool *pgxpool.Pool) *PostgresTxManager {
	return &PostgresTxManager{pool: pool}
}

// WithinTransaction begins a database transaction and binds it to ctx for child repository calls.
func (m *PostgresTxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	// If a transaction is already in flight, reuse it without nesting
	if TxFromContext(ctx) != nil {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txCtx := WithTx(ctx, tx)
	if err := fn(txCtx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
