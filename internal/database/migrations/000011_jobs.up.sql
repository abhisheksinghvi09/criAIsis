-- Durable work queue. A row is the job; a worker holds it through a time-limited
-- lease it keeps extending while it runs. If the worker dies the lease lapses and
-- another claims the row, so work is at-least-once across crashes and replicas.

CREATE TABLE jobs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    incident_id      UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    status           VARCHAR(16) NOT NULL DEFAULT 'queued',
    attempts         INT NOT NULL DEFAULT 0,
    max_attempts     INT NOT NULL DEFAULT 5,
    run_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_by        VARCHAR(64),
    lease_expires_at TIMESTAMPTZ,
    last_error       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at      TIMESTAMPTZ,

    CONSTRAINT chk_jobs_status CHECK (status IN ('queued', 'running', 'succeeded', 'dead')),
    CONSTRAINT chk_jobs_attempts CHECK (attempts >= 0 AND max_attempts >= 1),
    CONSTRAINT chk_jobs_running_has_lease CHECK (
        status <> 'running' OR (locked_by IS NOT NULL AND lease_expires_at IS NOT NULL)
    )
);

-- One live job per incident: re-enqueueing is idempotent.
CREATE UNIQUE INDEX uq_jobs_active_incident ON jobs(incident_id) WHERE status IN ('queued', 'running');

-- The claim scan: eligible queued work, and running work whose lease lapsed.
CREATE INDEX idx_jobs_queued ON jobs(run_at) WHERE status = 'queued';
CREATE INDEX idx_jobs_running ON jobs(lease_expires_at) WHERE status = 'running';
-- Per-tenant concurrency counts.
CREATE INDEX idx_jobs_workspace_running ON jobs(workspace_id) WHERE status = 'running';
-- Retention sweep.
CREATE INDEX idx_jobs_finished ON jobs(finished_at) WHERE status IN ('succeeded', 'dead');
