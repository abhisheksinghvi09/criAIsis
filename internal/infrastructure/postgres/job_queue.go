package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"criaisis/internal/domain/value"
	"criaisis/internal/job"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// JobQueueConfig tunes the durable queue. Zero values take the defaults below.
type JobQueueConfig struct {
	Workers        int           // concurrent jobs in this process (default 4)
	Lease          time.Duration // a claim lapses this long after the last heartbeat (default 2m)
	MaxAttempts    int           // executions before a job is dead-lettered (default 5)
	BaseBackoff    time.Duration // first retry delay before jitter (default 5s)
	MaxBackoff     time.Duration // retry delay ceiling (default 5m)
	PollInterval   time.Duration // idle wait between claims (default 1s)
	PerTenantLimit int           // concurrent jobs one workspace may hold (default 2)
	Retention      time.Duration // how long finished jobs are kept (default 7d)
}

func (c JobQueueConfig) withDefaults() JobQueueConfig {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	defInt := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	defInt(&c.Workers, 4)
	defInt(&c.MaxAttempts, 5)
	defInt(&c.PerTenantLimit, 2)
	def(&c.Lease, 2*time.Minute)
	def(&c.BaseBackoff, 5*time.Second)
	def(&c.MaxBackoff, 5*time.Minute)
	def(&c.PollInterval, time.Second)
	def(&c.Retention, 7*24*time.Hour)
	return c
}

// errLeaseLost cancels a job's context when another worker has taken it over.
var errLeaseLost = errors.New("job lease lost")

// PostgresJobQueue is a durable, multi-replica work queue.
//
// Claiming uses FOR UPDATE SKIP LOCKED, so concurrent workers never block on or
// double-claim a row. A claim is a lease the worker renews while it runs; a lapsed
// lease (crashed worker) makes the row claimable again, which is what gives
// at-least-once execution across crashes. Failures retry with exponential backoff
// and equal jitter, then dead-letter after MaxAttempts. Admission is fair across
// tenants with an exact per-tenant concurrency cap (see claim), so one workspace
// flooding the queue cannot starve the rest.
//
// ponytail: idle workers poll (plus an in-process wake on Enqueue) instead of
// using LISTEN/NOTIFY. Worst-case pickup latency from another replica is one
// PollInterval; switch to LISTEN/NOTIFY if that ever matters.
type PostgresJobQueue struct {
	pool *pgxpool.Pool
	cfg  JobQueueConfig
	log  *zerolog.Logger
	id   string

	wake chan struct{}
	stop chan struct{}
	once sync.Once
	wg   sync.WaitGroup

	jobCtx     context.Context
	cancelJobs context.CancelFunc
	onDead     func(context.Context, job.IncidentJob)

	depth, dead atomic.Int64
}

var _ job.Queue = (*PostgresJobQueue)(nil)

// NewJobQueue builds the queue. Nothing runs until Start.
func NewJobQueue(pool *pgxpool.Pool, cfg JobQueueConfig, log *zerolog.Logger) *PostgresJobQueue {
	host, _ := os.Hostname()
	return &PostgresJobQueue{
		pool: pool,
		cfg:  cfg.withDefaults(),
		log:  log,
		id:   fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.NewString()[:6]),
		wake: make(chan struct{}, 1),
		stop: make(chan struct{}),
	}
}

// OnDead registers a callback for jobs dead-lettered without the handler seeing
// the final failure (their workers crashed through every attempt).
func (q *PostgresJobQueue) OnDead(fn func(context.Context, job.IncidentJob)) *PostgresJobQueue {
	q.onDead = fn
	return q
}

// Depth is the number of jobs waiting to run, as of the last housekeeping pass.
func (q *PostgresJobQueue) Depth() int { return int(q.depth.Load()) }

// DeadLetters is the number of jobs that exhausted their attempts.
func (q *PostgresJobQueue) DeadLetters() int { return int(q.dead.Load()) }

// Enqueue inserts a job. Called inside a transaction (ctx from the tx manager) it
// commits or rolls back with the incident it belongs to. A second live job for
// the same incident is a no-op, which makes producers safe to retry.
func (q *PostgresJobQueue) Enqueue(ctx context.Context, j job.IncidentJob) error {
	select {
	case <-q.stop:
		return job.ErrQueueClosed
	default:
	}

	_, err := GetExecutor(ctx, q.pool).Exec(ctx, `
		INSERT INTO jobs (workspace_id, incident_id, max_attempts)
		VALUES ($1, $2, $3)
		ON CONFLICT (incident_id) WHERE status IN ('queued', 'running') DO NOTHING`,
		j.WorkspaceID.UUID(), j.IncidentID, q.cfg.MaxAttempts)
	if err != nil {
		return fmt.Errorf("enqueueing job: %w", err)
	}

	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// Start launches the workers and the housekeeping loop. ctx cancelling aborts
// running jobs, which are released for immediate pickup elsewhere.
func (q *PostgresJobQueue) Start(ctx context.Context, handler job.Handler) {
	q.jobCtx, q.cancelJobs = context.WithCancel(ctx)

	for i := 0; i < q.cfg.Workers; i++ {
		q.wg.Add(1)
		go q.worker(ctx, fmt.Sprintf("%s/%d", q.id, i), handler)
	}
	q.wg.Add(1)
	go q.housekeeping(ctx)

	q.log.Info().Int("workers", q.cfg.Workers).Dur("lease", q.cfg.Lease).Int("max_attempts", q.cfg.MaxAttempts).
		Msg("durable job queue started")
}

// Shutdown stops claiming new work and lets running jobs finish. If ctx expires
// first they are cancelled and handed back to the queue unharmed.
func (q *PostgresJobQueue) Shutdown(ctx context.Context) error {
	q.once.Do(func() { close(q.stop) })

	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()

	select {
	case <-done:
		q.log.Info().Msg("job queue drained")
		return nil
	case <-ctx.Done():
		if q.cancelJobs != nil {
			q.cancelJobs() // running jobs release themselves
		}
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		return ctx.Err()
	}
}

type claimed struct {
	id          uuid.UUID
	job         job.IncidentJob
	attempts    int
	maxAttempts int
}

func (q *PostgresJobQueue) worker(ctx context.Context, workerID string, handler job.Handler) {
	defer q.wg.Done()
	for {
		select {
		case <-q.stop:
			return
		case <-ctx.Done():
			return
		default:
		}

		c, ok, err := q.claim(ctx, workerID)
		if err != nil {
			if ctx.Err() == nil {
				q.log.Error().Err(err).Msg("claiming job")
			}
			q.idle(ctx)
			continue
		}
		if !ok {
			q.idle(ctx)
			continue
		}
		q.run(workerID, c, handler)
	}
}

// idle waits for new work: an Enqueue in this process, the poll timer (jittered
// so replicas do not poll in lockstep), shutdown, or cancellation.
func (q *PostgresJobQueue) idle(ctx context.Context) {
	wait := q.cfg.PollInterval + time.Duration(jitter()*0.4*float64(q.cfg.PollInterval)) - q.cfg.PollInterval/5
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-q.wake:
	case <-t.C:
	case <-q.stop:
	case <-ctx.Done():
	}
}

// eligible is the predicate for a claimable row: due and queued, or running with
// a lapsed lease and attempts left (its worker crashed).
const eligible = `(
	(status = 'queued' AND run_at <= now())
	OR (status = 'running' AND lease_expires_at < now() AND attempts < max_attempts)
)`

// claim leases the next job, or reports none.
//
// It is fair across tenants and enforces the per-tenant cap exactly:
//  1. List tenants that have eligible work, least running first, then oldest
//     waiting job first.
//  2. For each, inside a short transaction, take a non-blocking advisory lock on
//     the tenant. Only one claimer can decide for a tenant at a time, so the
//     running count read next cannot be stale (two workers each seeing "0
//     running" and both claiming is exactly the race this closes). Different
//     tenants never wait on each other.
//  3. If the tenant is under its cap, lease its oldest eligible job.
func (q *PostgresJobQueue) claim(ctx context.Context, workerID string) (claimed, bool, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT j.workspace_id
		FROM jobs j
		WHERE `+eligible+`
		GROUP BY j.workspace_id
		ORDER BY (SELECT count(*) FROM jobs r
		          WHERE r.workspace_id = j.workspace_id AND r.status = 'running' AND r.lease_expires_at >= now()),
		         min(j.run_at)
		LIMIT 32`)
	if err != nil {
		return claimed{}, false, err
	}
	var tenants []uuid.UUID
	for rows.Next() {
		var ws uuid.UUID
		if err := rows.Scan(&ws); err != nil {
			rows.Close()
			return claimed{}, false, err
		}
		tenants = append(tenants, ws)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return claimed{}, false, err
	}

	for _, ws := range tenants {
		c, ok, err := q.claimFor(ctx, ws, workerID)
		if err != nil || ok {
			return c, ok, err
		}
	}
	return claimed{}, false, nil
}

// claimFor tries to lease one job for a single tenant under its admission lock.
func (q *PostgresJobQueue) claimFor(ctx context.Context, wsID uuid.UUID, workerID string) (claimed, bool, error) {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return claimed{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('criaisis.jobs:' || $1::text, 0))`, wsID).
		Scan(&locked); err != nil || !locked {
		return claimed{}, false, err // another worker is admitting this tenant right now
	}

	var running int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM jobs WHERE workspace_id = $1 AND status = 'running' AND lease_expires_at >= now()`, wsID).
		Scan(&running); err != nil {
		return claimed{}, false, err
	}
	if running >= q.cfg.PerTenantLimit {
		return claimed{}, false, nil
	}

	var c claimed
	err = tx.QueryRow(ctx, `
		UPDATE jobs SET
			status = 'running', attempts = attempts + 1, locked_by = $2,
			lease_expires_at = now() + make_interval(secs => $3), updated_at = now()
		WHERE id = (
			SELECT id FROM jobs
			WHERE workspace_id = $1 AND `+eligible+`
			ORDER BY run_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, incident_id, attempts, max_attempts`,
		wsID, workerID, q.cfg.Lease.Seconds()).
		Scan(&c.id, &c.job.IncidentID, &c.attempts, &c.maxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return claimed{}, false, nil
	}
	if err != nil {
		return claimed{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return claimed{}, false, err
	}

	ws, err := value.FromUUID(wsID)
	if err != nil {
		return claimed{}, false, fmt.Errorf("invalid workspace id on job: %w", err)
	}
	c.job.WorkspaceID = ws
	c.job.Attempt, c.job.MaxAttempts = c.attempts, c.maxAttempts
	return c, true, nil
}

// run executes one claimed job under a heartbeat and records the outcome.
func (q *PostgresJobQueue) run(workerID string, c claimed, handler job.Handler) {
	ctx, cancel := context.WithCancelCause(q.jobCtx)
	defer cancel(nil)

	hbStop, hbDone := make(chan struct{}), make(chan struct{})
	go q.heartbeat(c.id, workerID, cancel, hbStop, hbDone)

	err := safeCall(ctx, handler, c.job)

	close(hbStop)
	<-hbDone

	// The outcome is written on a context the shutdown cannot cancel, or the very
	// signal that interrupted the job would also lose its bookkeeping.
	dbCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()

	switch {
	case errors.Is(context.Cause(ctx), errLeaseLost):
		q.log.Warn().Str("job", c.id.String()).Msg("lease lost; another worker owns this job now")
	case err == nil:
		q.finish(dbCtx, c, workerID, "succeeded", "", 0)
	case q.jobCtx.Err() != nil:
		q.release(dbCtx, c, workerID) // interrupted by shutdown, not a failure
	case job.IsPermanent(err) || c.attempts >= c.maxAttempts:
		q.log.Error().Err(err).Str("incident_id", c.job.IncidentID.String()).Int("attempt", c.attempts).Msg("job dead-lettered")
		q.finish(dbCtx, c, workerID, "dead", err.Error(), 0)
	default:
		delay := Backoff(c.attempts, q.cfg.BaseBackoff, q.cfg.MaxBackoff, jitter())
		q.log.Warn().Err(err).Str("incident_id", c.job.IncidentID.String()).Int("attempt", c.attempts).Dur("retry_in", delay).Msg("job failed; will retry")
		q.finish(dbCtx, c, workerID, "queued", err.Error(), delay)
	}
}

// safeCall turns a handler panic into an error so one bad job cannot kill a worker.
func safeCall(ctx context.Context, h job.Handler, j job.IncidentJob) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in job handler: %v", r)
		}
	}()
	return h(ctx, j)
}

// heartbeat renews the lease while the handler runs. If a renewal finds the row
// no longer ours, the job is cancelled: continuing would run it twice concurrently.
func (q *PostgresJobQueue) heartbeat(id uuid.UUID, workerID string, cancel context.CancelCauseFunc, stop, done chan struct{}) {
	defer close(done)
	t := time.NewTicker(q.cfg.Lease / 3)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
			tag, err := q.pool.Exec(ctx, `
				UPDATE jobs SET lease_expires_at = now() + make_interval(secs => $3), updated_at = now()
				WHERE id = $1 AND locked_by = $2 AND status = 'running'`, id, workerID, q.cfg.Lease.Seconds())
			c()
			switch {
			case err != nil:
				q.log.Warn().Err(err).Str("job", id.String()).Msg("lease renewal failed; will retry")
			case tag.RowsAffected() == 0:
				cancel(errLeaseLost)
				return
			}
		}
	}
}

// finish records a terminal or retry outcome. Every write is fenced on locked_by,
// so a worker that lost its lease can never overwrite the new owner's result.
func (q *PostgresJobQueue) finish(ctx context.Context, c claimed, workerID, status, lastErr string, delay time.Duration) {
	if len(lastErr) > 2000 {
		lastErr = lastErr[:2000]
	}
	tag, err := q.pool.Exec(ctx, `
		UPDATE jobs SET
			status = $3::text, last_error = $4, updated_at = now(),
			run_at = CASE WHEN $3::text = 'queued' THEN now() + make_interval(secs => $5) ELSE run_at END,
			finished_at = CASE WHEN $3::text IN ('succeeded', 'dead') THEN now() ELSE NULL END,
			locked_by = NULL, lease_expires_at = NULL
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`,
		c.id, workerID, status, lastErr, delay.Seconds())
	if err != nil {
		// The lease will lapse and the job will run again: safe, just late.
		q.log.Error().Err(err).Str("job", c.id.String()).Msg("recording job outcome failed")
		return
	}
	if tag.RowsAffected() == 0 {
		q.log.Warn().Str("job", c.id.String()).Msg("job outcome not recorded: lease was lost")
	}
}

// release hands an interrupted job straight back without spending an attempt.
func (q *PostgresJobQueue) release(ctx context.Context, c claimed, workerID string) {
	_, err := q.pool.Exec(ctx, `
		UPDATE jobs SET status = 'queued', attempts = GREATEST(attempts - 1, 0), run_at = now(),
		       locked_by = NULL, lease_expires_at = NULL, updated_at = now()
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`, c.id, workerID)
	if err != nil {
		q.log.Error().Err(err).Str("job", c.id.String()).Msg("releasing interrupted job failed")
	}
}

// housekeeping dead-letters jobs whose workers crashed through every attempt,
// prunes finished rows, and refreshes the gauges.
func (q *PostgresJobQueue) housekeeping(ctx context.Context) {
	defer q.wg.Done()
	for {
		q.sweep(ctx)
		t := time.NewTimer(10*time.Second + time.Duration(jitter()*float64(5*time.Second)))
		select {
		case <-t.C:
		case <-q.stop:
			t.Stop()
			return
		case <-ctx.Done():
			t.Stop()
			return
		}
	}
}

func (q *PostgresJobQueue) sweep(ctx context.Context) {
	rows, err := q.pool.Query(ctx, `
		UPDATE jobs SET status = 'dead', finished_at = now(), updated_at = now(),
		       last_error = 'worker lost the lease on every attempt', locked_by = NULL, lease_expires_at = NULL
		WHERE status = 'running' AND lease_expires_at < now() AND attempts >= max_attempts
		RETURNING workspace_id, incident_id, attempts, max_attempts`)
	if err != nil {
		q.log.Error().Err(err).Msg("reaping exhausted jobs")
	} else {
		var dead []job.IncidentJob
		for rows.Next() {
			var ws, inc uuid.UUID
			var a, m int
			if rows.Scan(&ws, &inc, &a, &m) == nil {
				if wsID, err := value.FromUUID(ws); err == nil {
					dead = append(dead, job.IncidentJob{WorkspaceID: wsID, IncidentID: inc, Attempt: a, MaxAttempts: m})
				}
			}
		}
		rows.Close()
		for _, j := range dead {
			q.log.Error().Str("incident_id", j.IncidentID.String()).Msg("job dead-lettered: workers kept crashing")
			if q.onDead != nil {
				q.onDead(ctx, j)
			}
		}
	}

	_, _ = q.pool.Exec(ctx, `
		DELETE FROM jobs WHERE id IN (
			SELECT id FROM jobs WHERE status = 'succeeded' AND finished_at < now() - make_interval(secs => $1) LIMIT 500)`,
		q.cfg.Retention.Seconds())

	var queued, dead int64
	if err := q.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'queued'), count(*) FILTER (WHERE status = 'dead') FROM jobs`).
		Scan(&queued, &dead); err == nil {
		q.depth.Store(queued)
		q.dead.Store(dead)
	}
}

// jitter is a uniform value in [0,1) for spreading retries and polls. It only
// decorrelates timing, so a fast non-cryptographic source is the right tool.
func jitter() float64 {
	return rand.Float64() //nolint:gosec // scheduling jitter, not a secret
}

// Backoff is the retry delay after the given attempt: exponential growth capped at
// max, then "equal jitter": half fixed, half random. The fixed half keeps a floor
// under the delay so a failing dependency is not hammered, and the random half
// spreads retries so a burst of failures does not return as a burst. rnd is a
// uniform value in [0,1).
func Backoff(attempt int, base, max time.Duration, rnd float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exp := base
	for i := 1; i < attempt && exp < max; i++ {
		exp *= 2
	}
	if exp > max {
		exp = max
	}
	half := exp / 2
	return half + time.Duration(rnd*float64(exp-half))
}
