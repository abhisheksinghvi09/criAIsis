CREATE TABLE IF NOT EXISTS users (
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
CREATE INDEX IF NOT EXISTS idx_users_workspace_id ON users(workspace_id);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(workspace_id, email);
CREATE TRIGGER set_updated_at_users BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

ALTER TABLE debate_turns DROP CONSTRAINT IF EXISTS chk_turns_stage, DROP CONSTRAINT IF EXISTS chk_turns_type;
ALTER TABLE documents DROP CONSTRAINT IF EXISTS chk_documents_status;

ALTER TABLE jobs DROP CONSTRAINT fk_jobs_incident_tenant,
    ADD CONSTRAINT jobs_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE;
ALTER TABLE sandbox_reproductions DROP CONSTRAINT fk_sandbox_incident_tenant,
    ADD CONSTRAINT sandbox_reproductions_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE;
ALTER TABLE debate_turns DROP CONSTRAINT fk_turns_persona_tenant, DROP CONSTRAINT fk_turns_incident_tenant,
    ADD CONSTRAINT debate_turns_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE,
    ADD CONSTRAINT debate_turns_persona_id_fkey FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE SET NULL;
ALTER TABLE document_chunks DROP CONSTRAINT fk_chunks_persona_tenant, DROP CONSTRAINT fk_chunks_document_tenant,
    ADD CONSTRAINT document_chunks_document_id_fkey FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE,
    ADD CONSTRAINT document_chunks_persona_id_fkey FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE;
ALTER TABLE documents DROP CONSTRAINT fk_documents_persona_tenant,
    ADD CONSTRAINT documents_persona_id_fkey FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE;

ALTER TABLE incidents DROP CONSTRAINT uq_incidents_id_workspace;
ALTER TABLE documents DROP CONSTRAINT uq_documents_id_workspace;
ALTER TABLE personas  DROP CONSTRAINT uq_personas_id_workspace;
