# Operating criAIsis

What to run, what to watch, and what to do when an alert fires. Every alert in
`deployments/prometheus/alerts.yml` links to a section here.

## How work flows, and why it survives failures

1. An alert or `/criaisis investigate` writes the incident **and** its job in one
   transaction. Neither exists without the other.
2. A worker claims the job with `FOR UPDATE SKIP LOCKED` and holds a **lease**
   (2 min), renewed every ~40s while the clash runs.
3. If the worker or process dies, the lease lapses and another worker (in this
   or another replica) reclaims the job. Execution is at-least-once.
4. A failure retries with exponential backoff and jitter (4 attempts). A permanent
   failure, such as missing credentials, goes to `dead` at once and the incident
   becomes `failed`.
5. The transcript is written in one transaction at the end of the clash. A retry
   that finds a complete transcript skips the debate, so retries never duplicate it.
6. Tenants are admitted fairly: a workspace may run at most 2 jobs at once, and
   the least-loaded tenant goes first.

On SIGTERM the process stops claiming, lets running jobs finish for 30s, then
hands any still running back to the queue without spending an attempt.

## Configuration that matters in production

| Variable | Purpose |
|---|---|
| `CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY` | Seals every tenant credential. Use `base64:` + 32 random bytes. **Back it up separately from database backups.** |
| `CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY_ID` / `..._PREVIOUS_ENCRYPTION_KEYS` | Key rotation (see below). |
| `CRIAISIS_SERVER_METRICS_TOKEN` | Enables `GET /metrics` behind this bearer token. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Enables tracing (OTLP/HTTP). |
| `CRIAISIS_FEATURES_SANDBOX` | Keep `false`. The sandbox needs host Docker and is not production-ready. |
| `CRIAISIS_DATABASE_SSL_MODE` | Must not be `disable` when `CRIAISIS_PRIMARY_ENV=production` (enforced). |

Probes: liveness `GET /healthz`, readiness `GET /readyz` (checks the database).

## Backups and recovery

**Primary: managed Postgres with point-in-time recovery** (continuous WAL
archiving, at least 7 days). This gives an RPO of seconds. Nightly logical dumps
are the second, independent line of defence. They protect against a bad PITR
configuration and give a portable copy.

```bash
# Nightly (cron, Kubernetes CronJob, ...). Keeps the 7 newest dumps plus the newest
# of each of the last 6 months; every dump is verified before it is kept.
PGHOST=... PGUSER=... PGPASSWORD=... PGDATABASE=criaisis scripts/backup.sh /backups

# Weekly: prove the backups actually restore. It restores into a scratch database,
# compares every table's row count and the migration version, then drops it.
PGHOST=... scripts/restore_drill.sh
```

**To recover:**

1. Restore beside the live database, never over it:
   `scripts/restore.sh /backups/criaisis-<stamp>.dump criaisis_restored`. This
   refuses an existing name.
2. Check the restored copy (`scripts/restore_drill.sh` logic, or spot-check the
   incidents).
3. Point `CRIAISIS_DATABASE_NAME` at it and roll the deployment. Migrations apply
   on boot if the dump is older than the code.
4. Investigations lost in the gap re-run automatically. On boot, any recent
   incident with no transcript and no live job is re-queued; older ones are
   marked failed.

A dump without the encryption key cannot decrypt tenant credentials. Tenants
would have to re-enter their keys.

## Rotating the credential encryption key

1. Generate a key: `echo "base64:$(openssl rand -base64 32)"`.
2. Set it as `CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY` with a new
   `..._KEY_ID` (for example `2`). Move the old key into
   `CRIAISIS_SECURITY_PREVIOUS_ENCRYPTION_KEYS=1:<old key>`.
3. Deploy. New writes use the new key, and old values still open.
4. Run `criaisis -rotate-credentials`. It re-seals every value under the new key
   in one locked transaction. It is all-or-nothing and safe to rerun.
5. Remove the old key from `..._PREVIOUS_ENCRYPTION_KEYS` and deploy.

Every value is bound to its tenant and field. A ciphertext copied into another
row will not decrypt.

## Alerts

### api-down
`/healthz` is unreachable. Check the process and its logs. Incoming alerts get
connection errors and providers retry them. Nothing is lost while the database
is up, because ingestion is idempotent per payload for 10 minutes.

### dead-letters
An investigation used up its attempts, so the people it was meant to reach got
nothing.
```sql
SELECT j.incident_id, j.attempts, j.last_error, j.finished_at
FROM jobs j WHERE j.status = 'dead' ORDER BY j.finished_at DESC LIMIT 20;
```
Fix the cause (usually a tenant credential or a model outage). To re-run one:
```sql
UPDATE incidents SET status = 'investigating' WHERE id = '<incident>';
INSERT INTO jobs (workspace_id, incident_id)
SELECT workspace_id, id FROM incidents WHERE id = '<incident>';
```

### queue-backlog
More than 50 jobs have been waiting for 10 minutes. Check `criaisis_investigation_duration_seconds`.
If model calls are slow, it is the provider. If not, add replicas. The queue is
shared and every replica claims from it.

### failing-investigations
Look at `last_error` on recent jobs and at traces for failing `clash.specialist`
spans. A model or embeddings outage shows up as the same error across tenants;
one tenant's bad key shows up for that tenant only.

### slow-investigations
p95 is over 60s. Traces show which specialist or the synthesis call is slow.

### api-errors
More than 5% of responses are 5xx. The access log has `request_id` and `trace_id`
on every line; find the failing route in `criaisis_http_requests_total`.
