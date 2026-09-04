-- 1. Incidents (Debate Sessions)
CREATE TABLE incidents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    slack_channel_id VARCHAR(64) NOT NULL,
    slack_thread_ts VARCHAR(64) NOT NULL,
    severity VARCHAR(32) NOT NULL DEFAULT 'sev-2',
    status VARCHAR(32) NOT NULL DEFAULT 'investigating',
    created_by_slack_user_id VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_workspace_channel_thread UNIQUE (workspace_id, slack_channel_id, slack_thread_ts)
);

CREATE INDEX idx_incidents_workspace_status ON incidents(workspace_id, status, created_at DESC);
CREATE INDEX idx_incidents_workspace_severity ON incidents(workspace_id, severity, created_at DESC);

CREATE TRIGGER set_updated_at_incidents
    BEFORE UPDATE ON incidents
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();

-- 2. Debate Turns (Debate Messages & Excerpt Citations)
CREATE TABLE debate_turns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    persona_id UUID REFERENCES personas(id) ON DELETE SET NULL,
    stage INT NOT NULL,
    turn_type VARCHAR(32) NOT NULL,
    content TEXT NOT NULL,
    referenced_chunk_ids UUID[] NOT NULL DEFAULT '{}',
    slack_message_ts VARCHAR(64),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_debate_turns_incident_order ON debate_turns(incident_id, created_at ASC);
CREATE INDEX idx_debate_turns_workspace_created ON debate_turns(workspace_id, created_at DESC);
