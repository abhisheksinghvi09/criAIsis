package job

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
)

// MemoryQueue is a buffered channel fronting a fixed worker pool.
//
// ponytail: single process, so jobs are lost if the binary dies mid-clash. That is
// acceptable while a debate is cheap to re-trigger from Slack; swap in a durable
// queue behind the Queue interface when it is not.
type MemoryQueue struct {
	jobs      chan IncidentJob
	workers   int
	log       *zerolog.Logger
	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    chan struct{}

	// closeMu excludes Enqueue from the moment Shutdown decides to close q.jobs.
	// Without it, a send past the (non-blocking) closed check can race the close
	// itself and panic with "send on closed channel".
	closeMu sync.RWMutex
}

var _ Queue = (*MemoryQueue)(nil)

// NewMemoryQueue builds a queue with the given buffer depth and worker count.
func NewMemoryQueue(buffer, workers int, log *zerolog.Logger) *MemoryQueue {
	if buffer < 1 {
		buffer = 1
	}
	if workers < 1 {
		workers = 1
	}
	return &MemoryQueue{
		jobs:    make(chan IncidentJob, buffer),
		workers: workers,
		log:     log,
		closed:  make(chan struct{}),
	}
}

// Enqueue submits a job without blocking. A full buffer is reported to the caller
// so the HTTP edge can shed load instead of holding a connection open.
func (q *MemoryQueue) Enqueue(job IncidentJob) error {
	q.closeMu.RLock()
	defer q.closeMu.RUnlock()

	select {
	case <-q.closed:
		return ErrQueueClosed
	default:
	}

	select {
	case q.jobs <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

// Start launches the worker pool.
func (q *MemoryQueue) Start(ctx context.Context, handler Handler) {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.work(ctx, handler)
	}
	q.log.Info().Int("workers", q.workers).Int("buffer", cap(q.jobs)).Msg("job queue started")
}

// work drains the queue until it is closed or the context is cancelled.
func (q *MemoryQueue) work(ctx context.Context, handler Handler) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-q.jobs:
			if !ok {
				return
			}
			q.run(ctx, handler, job)
		}
	}
}

// run executes one job, containing panics so a single bad incident cannot take
// down the worker pool mid-outage.
func (q *MemoryQueue) run(ctx context.Context, handler Handler, job IncidentJob) {
	defer func() {
		if r := recover(); r != nil {
			q.log.Error().Interface("panic", r).Str("incident_id", job.IncidentID.String()).Msg("recovered from panic in job handler")
		}
	}()

	if err := handler(ctx, job); err != nil {
		q.log.Error().Err(err).Str("incident_id", job.IncidentID.String()).Msg("job failed")
	}
}

// Shutdown stops intake and waits for in-flight work, bounded by ctx.
func (q *MemoryQueue) Shutdown(ctx context.Context) error {
	q.closeOnce.Do(func() {
		q.closeMu.Lock()
		close(q.closed)
		close(q.jobs)
		q.closeMu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		q.log.Info().Msg("job queue drained")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
