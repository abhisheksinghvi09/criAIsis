package repository

import (
	"context"
)

// TransactionManager coordinates atomic ACID transaction boundaries spanning multiple repository calls.
// Implementations bind transactions to context.Context so business logic remains decoupled from SQL mechanics.
type TransactionManager interface {
	// WithinTransaction executes the callback function inside a database transaction.
	// If fn returns nil, the transaction commits. If fn returns an error, it automatically rolls back.
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
