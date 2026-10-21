// Package job carries incident work off the HTTP request path so ingress can
// acknowledge inside Slack's timeout while the clash runs behind it.
package job

import (
	"context"
	"errors"

	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// ErrQueueFull signals backpressure: the buffer is saturated and the caller should
// shed load at the edge rather than block an HTTP handler.
var ErrQueueFull = errors.New("job queue is full")

// ErrQueueClosed is returned once the queue has been shut down.
var ErrQueueClosed = errors.New("job queue is closed")

// IncidentJob is a request to run the 2-stage clash for one persisted incident.
// It carries identifiers only: the worker reloads state, so a job never holds a
// stale copy of an incident that was resolved between enqueue and execution.
type IncidentJob struct {
	WorkspaceID value.WorkspaceID
	IncidentID  uuid.UUID

	// Attempt is this execution's number (1 for the first); MaxAttempts is how many
	// the queue will make before dead-lettering the job. A queue that does not retry
	// leaves both at 1.
	Attempt     int
	MaxAttempts int
}

// FinalAttempt reports whether a failure now is terminal: no retry will follow.
func (j IncidentJob) FinalAttempt() bool { return j.Attempt >= j.MaxAttempts }

// permanentError marks a failure that retrying cannot fix.
type permanentError struct{ error }

func (e permanentError) Unwrap() error { return e.error }

// Permanent wraps err so the queue dead-letters the job at once instead of
// retrying it: a missing credential does not appear by waiting.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err}
}

// IsPermanent reports whether err was marked with Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// Handler processes one job. Returning an error makes a retrying queue schedule
// another attempt (unless the error is Permanent or this was the final attempt);
// it never stops the pool. The handler must therefore be idempotent.
type Handler func(ctx context.Context, job IncidentJob) error

// Queue decouples ingress from execution. The Postgres implementation is the
// durable one used in production; the in-memory one serves tests.
type Queue interface {
	// Enqueue submits a job. With a durable queue it joins the transaction carried
	// by ctx, so the job exists if and only if the incident row does. It may
	// return ErrQueueFull under saturation.
	Enqueue(ctx context.Context, job IncidentJob) error

	// Start launches the worker pool. It returns immediately.
	Start(ctx context.Context, handler Handler)

	// Shutdown stops accepting work and waits for in-flight jobs to finish or for
	// ctx to expire, whichever comes first.
	Shutdown(ctx context.Context) error
}
