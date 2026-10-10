package job_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"criaisis/internal/domain/value"
	"criaisis/internal/job"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func newQueue(buffer, workers int) *job.MemoryQueue {
	logger := zerolog.New(io.Discard)
	return job.NewMemoryQueue(buffer, workers, &logger)
}

func testJob() job.IncidentJob {
	return job.IncidentJob{WorkspaceID: value.NewWorkspaceID(), IncidentID: uuid.New()}
}

func TestMemoryQueue_ProcessesEveryJob(t *testing.T) {
	q := newQueue(16, 4)
	var processed atomic.Int64
	done := make(chan struct{})

	q.Start(context.Background(), func(context.Context, job.IncidentJob) error {
		if processed.Add(1) == 10 {
			close(done)
		}
		return nil
	})

	for i := 0; i < 10; i++ {
		if err := q.Enqueue(testJob()); err != nil {
			t.Fatalf("enqueue %d failed: %v", i, err)
		}
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("only %d of 10 jobs processed", processed.Load())
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Shutdown(ctx); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

// A saturated queue must report backpressure rather than block the HTTP handler,
// which is what keeps the sub-500ms acknowledgement promise under a webhook burst.
func TestMemoryQueue_EnqueueDoesNotBlockWhenFull(t *testing.T) {
	q := newQueue(2, 1) // not started: nothing drains it

	if err := q.Enqueue(testJob()); err != nil {
		t.Fatalf("first enqueue should succeed: %v", err)
	}
	if err := q.Enqueue(testJob()); err != nil {
		t.Fatalf("second enqueue should succeed: %v", err)
	}

	start := time.Now()
	err := q.Enqueue(testJob())
	elapsed := time.Since(start)

	if !errors.Is(err, job.ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("enqueue blocked for %v; it must return immediately", elapsed)
	}
}

// One bad incident must not take the worker pool down mid-outage.
func TestMemoryQueue_SurvivesPanickingHandler(t *testing.T) {
	q := newQueue(8, 1)
	var mu sync.Mutex
	var seen int
	recovered := make(chan struct{})

	q.Start(context.Background(), func(_ context.Context, j job.IncidentJob) error {
		mu.Lock()
		seen++
		current := seen
		mu.Unlock()

		if current == 1 {
			panic("handler exploded")
		}
		close(recovered)
		return nil
	})

	_ = q.Enqueue(testJob())
	_ = q.Enqueue(testJob())

	select {
	case <-recovered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not survive a panicking handler")
	}
}

func TestMemoryQueue_RejectsAfterShutdown(t *testing.T) {
	q := newQueue(4, 1)
	q.Start(context.Background(), func(context.Context, job.IncidentJob) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	if err := q.Enqueue(testJob()); !errors.Is(err, job.ErrQueueClosed) {
		t.Errorf("expected ErrQueueClosed after shutdown, got %v", err)
	}
}
