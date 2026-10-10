ALTER TABLE workspaces DROP CONSTRAINT IF EXISTS chk_workspaces_notify_provider;
ALTER TABLE workspaces DROP COLUMN IF EXISTS notify_webhook_url_encrypted;
ALTER TABLE workspaces DROP COLUMN IF EXISTS notify_provider;
