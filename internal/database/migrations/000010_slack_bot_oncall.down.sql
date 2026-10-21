DROP INDEX IF EXISTS idx_incidents_workspace_channel;

ALTER TABLE workspace_settings
    DROP CONSTRAINT IF EXISTS chk_settings_oncall_key,
    DROP CONSTRAINT IF EXISTS chk_settings_oncall_provider,
    DROP COLUMN IF EXISTS oncall_schedule_id,
    DROP COLUMN IF EXISTS oncall_api_key_encrypted,
    DROP COLUMN IF EXISTS oncall_provider;

UPDATE workspace_settings SET notify_provider = 'none' WHERE notify_provider = 'slack_bot';
ALTER TABLE workspace_settings DROP CONSTRAINT chk_settings_notify_url;
ALTER TABLE workspace_settings DROP CONSTRAINT chk_settings_notify_provider;
ALTER TABLE workspace_settings
    ADD CONSTRAINT chk_settings_notify_provider CHECK (notify_provider IN ('none', 'slack', 'discord')),
    ADD CONSTRAINT chk_settings_notify_url CHECK (
        notify_provider = 'none' OR notify_webhook_url_encrypted IS NOT NULL
    );
