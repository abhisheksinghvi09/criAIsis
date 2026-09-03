-- 1. Workspaces (Tenants / Slack Teams)
CREATE TABLE workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slack_team_id VARCHAR(64) UNIQUE NOT NULL,
    slack_team_name VARCHAR(255) NOT NULL,
    slack_bot_token_encrypted BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_workspaces_slack_team_id ON workspaces(slack_team_id);

CREATE TRIGGER set_updated_at_workspaces
    BEFORE UPDATE ON workspaces
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();

-- 2. Users (Dashboard Operators & Workspace Members)
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    slack_user_id VARCHAR(64) NOT NULL,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'member',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_workspace_slack_user UNIQUE (workspace_id, slack_user_id)
);

CREATE INDEX idx_users_workspace_id ON users(workspace_id);
CREATE INDEX idx_users_email ON users(workspace_id, email);

CREATE TRIGGER set_updated_at_users
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();

-- 3. Personas (Fixed 4 domain specialists per workspace)
CREATE TABLE personas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    key VARCHAR(32) NOT NULL,
    display_name VARCHAR(64) NOT NULL,
    system_prompt TEXT NOT NULL,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_workspace_persona_key UNIQUE (workspace_id, key)
);

CREATE INDEX idx_personas_workspace_id ON personas(workspace_id);

CREATE TRIGGER set_updated_at_personas
    BEFORE UPDATE ON personas
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();

