package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBExecutor abstracts pgxpool.Pool and pgx.Tx to allow seamless execution
// across standalone queries and coordinated multi-table transactions.
type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

type txKey struct{}

// WithTx injects an active database transaction into the execution context.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFromContext extracts an active transaction if one was initiated by TransactionManager.
func TxFromContext(ctx context.Context) pgx.Tx {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return nil
}

// GetExecutor returns the scoped transaction from context if present, falling back to the connection pool.
func GetExecutor(ctx context.Context, pool *pgxpool.Pool) DBExecutor {
	if tx := TxFromContext(ctx); tx != nil {
		return tx
	}
	if pool == nil {
		return nil
	}
	return pool
}
