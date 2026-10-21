package repository

import (
	"context"
	"errors"
)

// ErrConflict means an optimistic update lost a race: the row changed after it
// was read. The caller should reload and reapply its change.
var ErrConflict = errors.New("concurrent modification: reload and retry")

// TransactionManager coordinates atomic ACID transaction boundaries spanning multiple repository calls.
// Implementations bind transactions to context.Context so business logic remains decoupled from SQL mechanics.
type TransactionManager interface {
	// WithinTransaction executes the callback function inside a database transaction.
	// If fn returns nil, the transaction commits. If fn returns an error, it automatically rolls back.
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// RetryOnConflict runs a read-modify-write until it commits without losing an
// optimistic race, up to 5 attempts. fn must reload what it modifies on every
// call: retrying a stale copy would conflict forever.
func RetryOnConflict(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = fn(); !errors.Is(err, ErrConflict) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return err
}
