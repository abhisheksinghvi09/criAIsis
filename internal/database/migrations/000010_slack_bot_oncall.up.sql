-- Deliver verdicts through a per-incident Slack channel opened by the installed
-- bot ('slack_bot'), which needs no webhook URL, and pull the people to invite
-- from the tenant's own PagerDuty or Opsgenie schedule.

ALTER TABLE workspace_settings DROP CONSTRAINT chk_settings_notify_provider;
ALTER TABLE workspace_settings DROP CONSTRAINT chk_settings_notify_url;
ALTER TABLE workspace_settings
    ADD CONSTRAINT chk_settings_notify_provider CHECK (notify_provider IN ('none', 'slack', 'discord', 'slack_bot')),
    ADD CONSTRAINT chk_settings_notify_url CHECK (
        notify_provider IN ('none', 'slack_bot') OR notify_webhook_url_encrypted IS NOT NULL
    );

ALTER TABLE workspace_settings
    ADD COLUMN oncall_provider          VARCHAR(16)  NOT NULL DEFAULT 'none',
    ADD COLUMN oncall_api_key_encrypted BYTEA,
    ADD COLUMN oncall_schedule_id       VARCHAR(128) NOT NULL DEFAULT '',
    ADD CONSTRAINT chk_settings_oncall_provider CHECK (oncall_provider IN ('none', 'pagerduty', 'opsgenie')),
    ADD CONSTRAINT chk_settings_oncall_key CHECK (
        oncall_provider = 'none' OR (oncall_api_key_encrypted IS NOT NULL AND oncall_schedule_id <> '')
    );

-- Follow-up @mentions arrive with a channel id; find the incident that owns it.
CREATE INDEX idx_incidents_workspace_channel ON incidents(workspace_id, slack_channel_id, created_at DESC);
