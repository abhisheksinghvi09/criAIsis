-- Tenant integrity in the schema, not only in the queries. Every child row
-- carries workspace_id, but its foreign keys pointed at the parent's id alone, so
-- a bug could attach tenant A's chunk to tenant B's persona and the database
-- would accept it. Composite keys make that impossible: a child must reference a
-- parent with the same workspace.

ALTER TABLE personas  ADD CONSTRAINT uq_personas_id_workspace  UNIQUE (id, workspace_id);
ALTER TABLE documents ADD CONSTRAINT uq_documents_id_workspace UNIQUE (id, workspace_id);
ALTER TABLE incidents ADD CONSTRAINT uq_incidents_id_workspace UNIQUE (id, workspace_id);

ALTER TABLE documents
    DROP CONSTRAINT documents_persona_id_fkey,
    ADD CONSTRAINT fk_documents_persona_tenant
        FOREIGN KEY (persona_id, workspace_id) REFERENCES personas(id, workspace_id) ON DELETE CASCADE;

ALTER TABLE document_chunks
    DROP CONSTRAINT document_chunks_document_id_fkey,
    DROP CONSTRAINT document_chunks_persona_id_fkey,
    ADD CONSTRAINT fk_chunks_document_tenant
        FOREIGN KEY (document_id, workspace_id) REFERENCES documents(id, workspace_id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_chunks_persona_tenant
        FOREIGN KEY (persona_id, workspace_id) REFERENCES personas(id, workspace_id) ON DELETE CASCADE;

-- SET NULL (persona_id) clears only the persona: workspace_id is NOT NULL.
ALTER TABLE debate_turns
    DROP CONSTRAINT debate_turns_incident_id_fkey,
    DROP CONSTRAINT debate_turns_persona_id_fkey,
    ADD CONSTRAINT fk_turns_incident_tenant
        FOREIGN KEY (incident_id, workspace_id) REFERENCES incidents(id, workspace_id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_turns_persona_tenant
        FOREIGN KEY (persona_id, workspace_id) REFERENCES personas(id, workspace_id) ON DELETE SET NULL (persona_id);

ALTER TABLE sandbox_reproductions
    DROP CONSTRAINT sandbox_reproductions_incident_id_fkey,
    ADD CONSTRAINT fk_sandbox_incident_tenant
        FOREIGN KEY (incident_id, workspace_id) REFERENCES incidents(id, workspace_id) ON DELETE CASCADE;

ALTER TABLE jobs
    DROP CONSTRAINT jobs_incident_id_fkey,
    ADD CONSTRAINT fk_jobs_incident_tenant
        FOREIGN KEY (incident_id, workspace_id) REFERENCES incidents(id, workspace_id) ON DELETE CASCADE;

-- Enumerations the application already treats as closed.
ALTER TABLE documents ADD CONSTRAINT chk_documents_status CHECK (status IN ('pending', 'indexed', 'failed'));
ALTER TABLE debate_turns
    ADD CONSTRAINT chk_turns_type CHECK (turn_type IN ('specialist_hypothesis', 'synthesis', 'follow_up')),
    ADD CONSTRAINT chk_turns_stage CHECK (stage IN (1, 2, 3));

-- Never used by any code path.
DROP TABLE IF EXISTS users;
