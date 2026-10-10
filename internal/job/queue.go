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
}

// Handler processes one job. Returning an error is logged by the worker; it does
// not stop the pool.
type Handler func(ctx context.Context, job IncidentJob) error

// Queue decouples ingress from execution. The in-memory implementation satisfies
// MVP volume; this interface is the seam where Redis would be swapped in.
type Queue interface {
	// Enqueue submits a job without blocking, returning ErrQueueFull under saturation.
	Enqueue(job IncidentJob) error

	// Start launches the worker pool. It returns immediately.
	Start(ctx context.Context, handler Handler)

	// Shutdown stops accepting work and waits for in-flight jobs to finish or for
	// ctx to expire, whichever comes first.
	Shutdown(ctx context.Context) error
}
