package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/postgres"
	"criaisis/internal/job"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

func TestBackoff_GrowsExponentiallyWithEqualJitterAndCaps(t *testing.T) {
	base, max := time.Second, 30*time.Second
	for attempt, want := range map[int][2]time.Duration{
		1: {500 * time.Millisecond, time.Second},
		2: {time.Second, 2 * time.Second},
		4: {4 * time.Second, 8 * time.Second},
		9: {15 * time.Second, 30 * time.Second}, // capped
	} {
		lo := postgres.Backoff(attempt, base, max, 0)
		hi := postgres.Backoff(attempt, base, max, 0.999999)
		if lo != want[0] || hi > want[1] || hi < want[1]-time.Millisecond {
			t.Errorf("attempt %d: range [%v, %v], want [%v, %v]", attempt, lo, hi, want[0], want[1])
		}
	}
}

// --- integration ---

type queueFixture struct {
	pool *pgxpool.Pool
	ws   []*entity.Workspace
}

func newQueueFixture(t *testing.T, tenants int) *queueFixture {
	t.Helper()
	return newQueueFixtureOn(t, getTestPool(t), tenants)
}

func newQueueFixtureOn(t *testing.T, pool *pgxpool.Pool, tenants int) *queueFixture {
	t.Helper()
	f := &queueFixture{pool: pool}
	repo := postgres.NewWorkspaceRepository(pool)
	for i := 0; i < tenants; i++ {
		ws, err := entity.NewWorkspace("T_Q_"+uuid.NewString()[:8], "Queue test", []byte("sealed"))
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Create(context.Background(), ws); err != nil {
			t.Fatal(err)
		}
		f.ws = append(f.ws, ws)
	}
	t.Cleanup(func() {
		for _, ws := range f.ws {
			_, _ = pool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", ws.ID().UUID())
		}
		pool.Close()
	})
	return f
}

func (f *queueFixture) incident(t *testing.T, tenant int) job.IncidentJob {
	t.Helper()
	ws := f.ws[tenant]
	inc, err := entity.NewIncident(ws.ID(), "queue test", "d", value.SlackChannelID("C1"),
		value.SlackThreadTS(uuid.NewString()), value.SeveritySev3, value.SlackUserID("U_TEST"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewIncidentRepository(f.pool).Create(context.Background(), inc); err != nil {
		t.Fatal(err)
	}
	return job.IncidentJob{WorkspaceID: ws.ID(), IncidentID: inc.ID()}
}

func (f *queueFixture) status(t *testing.T, incidentID uuid.UUID) (status string, attempts int) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(),
		`SELECT status, attempts FROM jobs WHERE incident_id = $1 ORDER BY created_at DESC LIMIT 1`, incidentID).
		Scan(&status, &attempts)
	if err != nil {
		t.Fatalf("reading job: %v", err)
	}
	return status, attempts
}

func fastQueue(pool *pgxpool.Pool, cfg postgres.JobQueueConfig) *postgres.PostgresJobQueue {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 20 * time.Millisecond
	}
	if cfg.BaseBackoff == 0 {
		cfg.BaseBackoff, cfg.MaxBackoff = 10*time.Millisecond, 20*time.Millisecond
	}
	log := zerolog.Nop()
	return postgres.NewJobQueue(pool, cfg, &log)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestJobQueue_EnqueueIsIdempotentPerIncident(t *testing.T) {
	f := newQueueFixture(t, 1)
	q := fastQueue(f.pool, postgres.JobQueueConfig{})
	j := f.incident(t, 0)

	for i := 0; i < 3; i++ {
		if err := q.Enqueue(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE incident_id = $1`, j.IncidentID).Scan(&n)
	if n != 1 {
		t.Errorf("%d jobs for one incident, want 1", n)
	}
}

func TestJobQueue_RetriesWithBackoffThenSucceeds(t *testing.T) {
	f := newQueueFixture(t, 1)
	q := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 1})
	j := f.incident(t, 0)
	_ = q.Enqueue(context.Background(), j)

	var calls atomic.Int32
	var seen []int
	var mu sync.Mutex
	q.Start(context.Background(), func(_ context.Context, got job.IncidentJob) error {
		mu.Lock()
		seen = append(seen, got.Attempt)
		mu.Unlock()
		if calls.Add(1) < 3 {
			return errors.New("transient")
		}
		return nil
	})
	defer func() { _ = q.Shutdown(context.Background()) }()

	waitFor(t, "success", func() bool { s, _ := f.status(t, j.IncidentID); return s == "succeeded" })
	if _, attempts := f.status(t, j.IncidentID); attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 || seen[0] != 1 || seen[2] != 3 {
		t.Errorf("handler saw attempts %v, want [1 2 3]", seen)
	}
}

func TestJobQueue_DeadLettersPermanentErrorsAndExhaustedRetries(t *testing.T) {
	f := newQueueFixture(t, 1)
	q := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 2, MaxAttempts: 3})
	permanent, flaky := f.incident(t, 0), f.incident(t, 0)
	_ = q.Enqueue(context.Background(), permanent)
	_ = q.Enqueue(context.Background(), flaky)

	q.Start(context.Background(), func(_ context.Context, got job.IncidentJob) error {
		if got.IncidentID == permanent.IncidentID {
			return job.Permanent(errors.New("no credentials"))
		}
		return errors.New("always fails")
	})
	defer func() { _ = q.Shutdown(context.Background()) }()

	waitFor(t, "both dead", func() bool {
		a, _ := f.status(t, permanent.IncidentID)
		b, _ := f.status(t, flaky.IncidentID)
		return a == "dead" && b == "dead"
	})
	if _, n := f.status(t, permanent.IncidentID); n != 1 {
		t.Errorf("permanent error retried: %d attempts", n)
	}
	if _, n := f.status(t, flaky.IncidentID); n != 3 {
		t.Errorf("flaky job made %d attempts, want 3", n)
	}
}

// A worker that crashed mid-job leaves a running row with a lapsed lease. The
// queue must pick it up again rather than leave it stranded.
func TestJobQueue_RecoversAJobWhoseWorkerCrashed(t *testing.T) {
	f := newQueueFixture(t, 1)
	j := f.incident(t, 0)
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO jobs (workspace_id, incident_id, status, attempts, locked_by, lease_expires_at)
		VALUES ($1, $2, 'running', 1, 'dead-worker', now() - interval '1 second')`,
		j.WorkspaceID.UUID(), j.IncidentID)
	if err != nil {
		t.Fatal(err)
	}

	q := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 1})
	var gotAttempt atomic.Int32
	q.Start(context.Background(), func(_ context.Context, got job.IncidentJob) error {
		gotAttempt.Store(int32(got.Attempt))
		return nil
	})
	defer func() { _ = q.Shutdown(context.Background()) }()

	waitFor(t, "recovery", func() bool { s, _ := f.status(t, j.IncidentID); return s == "succeeded" })
	if gotAttempt.Load() != 2 {
		t.Errorf("recovered run was attempt %d, want 2", gotAttempt.Load())
	}
}

// If another worker takes the row over, the original must be cancelled and must
// not be able to overwrite the new owner's outcome.
func TestJobQueue_FencesAWorkerThatLostItsLease(t *testing.T) {
	f := newQueueFixture(t, 1)
	j := f.incident(t, 0)
	q := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 1, Lease: 300 * time.Millisecond})
	_ = q.Enqueue(context.Background(), j)

	cancelled := make(chan error, 1)
	q.Start(context.Background(), func(ctx context.Context, _ job.IncidentJob) error {
		// Simulate another worker stealing the job.
		_, _ = f.pool.Exec(context.Background(), `UPDATE jobs SET locked_by = 'thief' WHERE incident_id = $1`, j.IncidentID)
		<-ctx.Done()
		cancelled <- context.Cause(ctx)
		return nil // would be "success" if it were allowed to record it
	})
	defer func() { _ = q.Shutdown(context.Background()) }()

	select {
	case cause := <-cancelled:
		if cause == nil || cause.Error() != "job lease lost" {
			t.Errorf("cancel cause = %v, want lease lost", cause)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker was not cancelled after losing its lease")
	}
	time.Sleep(100 * time.Millisecond)
	if s, _ := f.status(t, j.IncidentID); s != "running" {
		t.Errorf("fenced worker overwrote the owner's row: status %s", s)
	}
}

// A job interrupted by shutdown goes straight back to the queue with its attempt refunded.
func TestJobQueue_ShutdownReleasesInterruptedJobs(t *testing.T) {
	f := newQueueFixture(t, 1)
	j := f.incident(t, 0)
	q := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 1})
	_ = q.Enqueue(context.Background(), j)

	started := make(chan struct{})
	q.Start(context.Background(), func(ctx context.Context, _ job.IncidentJob) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = q.Shutdown(ctx)

	s, attempts := f.status(t, j.IncidentID)
	if s != "queued" || attempts != 0 {
		t.Errorf("after shutdown: status=%s attempts=%d, want queued/0", s, attempts)
	}
}

// One tenant flooding the queue must not starve another: with a per-tenant limit
// of one, the second worker serves tenant B even though A's jobs are older.
func TestJobQueue_SchedulesFairlyAcrossTenants(t *testing.T) {
	f := newQueueFixture(t, 2)
	var aJobs []job.IncidentJob
	for i := 0; i < 5; i++ {
		aJobs = append(aJobs, f.incident(t, 0))
	}
	b := f.incident(t, 1)

	q := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 2, PerTenantLimit: 1})
	for _, j := range aJobs {
		_ = q.Enqueue(context.Background(), j)
	}
	time.Sleep(20 * time.Millisecond)
	_ = q.Enqueue(context.Background(), b)

	var mu sync.Mutex
	var order []value.WorkspaceID
	release := make(chan struct{})
	q.Start(context.Background(), func(_ context.Context, got job.IncidentJob) error {
		mu.Lock()
		order = append(order, got.WorkspaceID)
		mu.Unlock()
		<-release
		return nil
	})
	defer func() { _ = q.Shutdown(context.Background()) }()

	waitFor(t, "two concurrent jobs", func() bool { mu.Lock(); defer mu.Unlock(); return len(order) == 2 })
	mu.Lock()
	first2 := append([]value.WorkspaceID(nil), order...)
	mu.Unlock()
	close(release)

	if first2[0] == first2[1] {
		t.Errorf("both slots went to one tenant (%v); tenant B was starved", first2)
	}
}

// Many workers racing over many jobs must run each exactly once.
func TestJobQueue_ConcurrentWorkersNeverDoubleClaim(t *testing.T) {
	f := newQueueFixture(t, 4)
	const perTenant = 10
	var all []job.IncidentJob
	for tenant := 0; tenant < 4; tenant++ {
		for i := 0; i < perTenant; i++ {
			j := f.incident(t, tenant)
			all = append(all, j)
		}
	}

	// Two queues = two replicas competing for the same rows.
	q1 := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 4, PerTenantLimit: 4})
	q2 := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 4, PerTenantLimit: 4})
	for _, j := range all {
		_ = q1.Enqueue(context.Background(), j)
	}

	var mu sync.Mutex
	runs := map[uuid.UUID]int{}
	h := func(_ context.Context, got job.IncidentJob) error {
		mu.Lock()
		runs[got.IncidentID]++
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		return nil
	}
	q1.Start(context.Background(), h)
	q2.Start(context.Background(), h)
	defer func() { _ = q1.Shutdown(context.Background()); _ = q2.Shutdown(context.Background()) }()

	waitFor(t, "all jobs", func() bool { mu.Lock(); defer mu.Unlock(); return len(runs) == len(all) })
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for id, n := range runs {
		if n != 1 {
			t.Errorf("incident %s ran %d times", id, n)
		}
	}
}

// The per-tenant cap is exact even with many workers across two replicas.
func TestJobQueue_PerTenantCapIsNeverExceeded(t *testing.T) {
	f := newQueueFixture(t, 1)
	for i := 0; i < 12; i++ {
		j := f.incident(t, 0)
		_ = postgres.NewJobQueue(f.pool, postgres.JobQueueConfig{}, ptrLog()).Enqueue(context.Background(), j)
	}

	var running, peak, done atomic.Int32
	h := func(context.Context, job.IncidentJob) error {
		now := running.Add(1)
		for {
			p := peak.Load()
			if now <= p || peak.CompareAndSwap(p, now) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
		done.Add(1)
		return nil
	}
	q1 := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 6, PerTenantLimit: 2})
	q2 := fastQueue(f.pool, postgres.JobQueueConfig{Workers: 6, PerTenantLimit: 2})
	q1.Start(context.Background(), h)
	q2.Start(context.Background(), h)
	defer func() { _ = q1.Shutdown(context.Background()); _ = q2.Shutdown(context.Background()) }()

	waitFor(t, "all 12 jobs", func() bool { return done.Load() == 12 })
	if peak.Load() > 2 {
		t.Errorf("tenant ran %d jobs at once; cap is 2", peak.Load())
	}
}

func ptrLog() *zerolog.Logger { l := zerolog.Nop(); return &l }

func TestRecoverOrphaned_RequeuesLostWorkAndLeavesFinishedInvestigationsAlone(t *testing.T) {
	// Recovery acts on every incident, so it gets a database of its own.
	f := newQueueFixtureOn(t, isolatedPool(t), 1)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}

	lost := f.incident(t, 0)     // investigating, no transcript, no job: lost work
	finished := f.incident(t, 0) // investigating but has a verdict: awaiting a human
	stale := f.incident(t, 0)    // lost work, but two days old
	queued := f.incident(t, 0)   // already has a live job
	exec(`INSERT INTO debate_turns (incident_id, workspace_id, stage, turn_type, content)
	      VALUES ($1, $2, 2, 'synthesis', 'verdict')`, finished.IncidentID, finished.WorkspaceID.UUID())
	exec(`UPDATE incidents SET created_at = now() - interval '2 days' WHERE id = $1`, stale.IncidentID)
	_ = fastQueue(f.pool, postgres.JobQueueConfig{}).Enqueue(ctx, queued)

	repo := postgres.NewIncidentRepository(f.pool)
	requeued, failed, err := repo.RecoverOrphaned(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if requeued != 1 || failed != 1 {
		t.Errorf("requeued=%d failed=%d, want 1 and 1", requeued, failed)
	}

	statusOf := func(id uuid.UUID) string {
		var s string
		_ = f.pool.QueryRow(ctx, `SELECT status FROM incidents WHERE id = $1`, id).Scan(&s)
		return s
	}
	jobsFor := func(id uuid.UUID) int {
		var n int
		_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE incident_id = $1`, id).Scan(&n)
		return n
	}

	if jobsFor(lost.IncidentID) != 1 || statusOf(lost.IncidentID) != "investigating" {
		t.Error("lost work was not requeued")
	}
	if jobsFor(finished.IncidentID) != 0 || statusOf(finished.IncidentID) != "investigating" {
		t.Error("a finished investigation awaiting resolution was touched")
	}
	if statusOf(stale.IncidentID) != "failed" || jobsFor(stale.IncidentID) != 0 {
		t.Error("stale lost work should be failed, not requeued")
	}
	if jobsFor(queued.IncidentID) != 1 {
		t.Error("an incident with a live job got a second one")
	}
}

// The schema, not just the queries, must refuse a row that points at another
// tenant's parent.
func TestSchema_RejectsCrossTenantReferences(t *testing.T) {
	f := newQueueFixture(t, 2)
	a := f.incident(t, 0)
	tenantB := f.ws[1].ID().UUID()
	ctx := context.Background()

	for name, sql := range map[string]string{
		"debate turn": `INSERT INTO debate_turns (incident_id, workspace_id, stage, turn_type, content) VALUES ($1, $2, 1, 'synthesis', 'x')`,
		"job":         `INSERT INTO jobs (incident_id, workspace_id) VALUES ($1, $2)`,
	} {
		_, err := f.pool.Exec(ctx, sql, a.IncidentID, tenantB)
		if err == nil || !strings.Contains(err.Error(), "23503") {
			t.Errorf("%s pointing at another tenant's incident: err = %v, want a foreign key violation", name, err)
		}
	}
}

// Two writers holding the same version: the first wins, the second gets
// ErrConflict instead of silently overwriting it.
func TestOptimisticConcurrency_RejectsAStaleWrite(t *testing.T) {
	f := newQueueFixture(t, 1)
	ctx := context.Background()
	j := f.incident(t, 0)
	repo := postgres.NewIncidentRepository(f.pool)

	human, _ := repo.GetByID(ctx, j.WorkspaceID, j.IncidentID)
	worker, _ := repo.GetByID(ctx, j.WorkspaceID, j.IncidentID)

	_ = human.Resolve(time.Now().UTC())
	if err := repo.Update(ctx, human); err != nil {
		t.Fatalf("first writer: %v", err)
	}
	_ = worker.Fail(time.Now().UTC())
	if err := repo.Update(ctx, worker); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale writer: err = %v, want ErrConflict", err)
	}

	// Retrying on fresh data sees the resolution and declines to fail it.
	err := repository.RetryOnConflict(ctx, func() error {
		fresh, err := repo.GetByID(ctx, j.WorkspaceID, j.IncidentID)
		if err != nil {
			return err
		}
		if err := fresh.Fail(time.Now().UTC()); err != nil {
			return err
		}
		return repo.Update(ctx, fresh)
	})
	if !errors.Is(err, entity.ErrNotInvestigating) {
		t.Errorf("retry err = %v, want ErrNotInvestigating", err)
	}
	final, _ := repo.GetByID(ctx, j.WorkspaceID, j.IncidentID)
	if final.Status() != value.IncidentStatusResolved {
		t.Errorf("final status %s: the human's resolution was overwritten", final.Status())
	}

	// Same guarantee for workspaces (alert-token rotation vs Slack reinstall).
	wsRepo := postgres.NewWorkspaceRepository(f.pool)
	a, _ := wsRepo.GetByID(ctx, f.ws[0].ID())
	b, _ := wsRepo.GetByID(ctx, f.ws[0].ID())
	_, _ = a.GenerateWebhookSecret()
	if err := wsRepo.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	_ = b.UpdateTeamName("renamed")
	if err := wsRepo.Update(ctx, b); !errors.Is(err, repository.ErrConflict) {
		t.Errorf("stale workspace write: err = %v, want ErrConflict", err)
	}
}
