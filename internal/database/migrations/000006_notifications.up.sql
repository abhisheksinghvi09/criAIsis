-- Outbound destination for a workspace's incident transcripts.
-- The webhook URL is a bearer credential (anyone holding it can post to the
-- channel), so it is stored encrypted with AES-GCM-256 like the bot token.
ALTER TABLE workspaces ADD COLUMN notify_provider VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE workspaces ADD COLUMN notify_webhook_url_encrypted BYTEA;

ALTER TABLE workspaces ADD CONSTRAINT chk_workspaces_notify_provider
    CHECK (notify_provider IN ('none', 'slack', 'discord'));
