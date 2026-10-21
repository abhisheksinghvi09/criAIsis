DROP INDEX IF EXISTS idx_workspaces_webhook;
ALTER TABLE workspaces DROP COLUMN IF EXISTS webhook_secret_hash;
