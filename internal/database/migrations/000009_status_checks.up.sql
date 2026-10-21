ALTER TABLE incidents
    ADD CONSTRAINT chk_incidents_status CHECK (status IN ('investigating', 'resolved', 'failed')),
    ADD CONSTRAINT chk_incidents_severity CHECK (severity IN ('sev-1', 'sev-2', 'sev-3', 'sev-4'));
