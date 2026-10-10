-- Per-workspace webhook credential for alert ingestion.
-- Only the SHA-256 digest is stored: the plaintext token is shown once at
-- generation time and is never recoverable from the database.
ALTER TABLE workspaces ADD COLUMN webhook_secret_hash BYTEA;

CREATE INDEX idx_workspaces_webhook ON workspaces(slack_team_id) WHERE webhook_secret_hash IS NOT NULL;
