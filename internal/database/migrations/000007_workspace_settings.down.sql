ALTER TABLE workspaces ADD COLUMN notify_provider VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE workspaces ADD COLUMN notify_webhook_url_encrypted BYTEA;

UPDATE workspaces w
SET notify_provider = s.notify_provider,
    notify_webhook_url_encrypted = s.notify_webhook_url_encrypted
FROM workspace_settings s
WHERE s.workspace_id = w.id;

ALTER TABLE workspaces ADD CONSTRAINT chk_workspaces_notify_provider
    CHECK (notify_provider IN ('none', 'slack', 'discord'));

ALTER TABLE workspaces DROP COLUMN IF EXISTS admin_api_key_hash;
DROP TABLE IF EXISTS workspace_settings;
