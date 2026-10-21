-- Sandbox Incident Reproduction
-- Tracks reproduction attempts of resolved incidents in isolated containers.

CREATE TABLE sandbox_reproductions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    status VARCHAR(64) NOT NULL DEFAULT 'Provisioning',
    scenario_id VARCHAR(128),
    container_ref VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ready_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_sandbox_status CHECK (status IN ('Provisioning', 'Ready', 'Failed', 'UnmatchedFallback'))
);

CREATE INDEX idx_sandbox_reproductions_incident ON sandbox_reproductions(incident_id, created_at DESC);
CREATE INDEX idx_sandbox_reproductions_workspace ON sandbox_reproductions(workspace_id, created_at DESC);

-- BR5.1: At most one SandboxReproduction may be in Provisioning or Ready status per incident at a time.
CREATE UNIQUE INDEX uq_sandbox_active_per_incident ON sandbox_reproductions(incident_id)
WHERE status IN ('Provisioning', 'Ready');

CREATE TRIGGER set_updated_at_sandbox_reproductions
    BEFORE UPDATE ON sandbox_reproductions
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
