ALTER TABLE incidents
    DROP CONSTRAINT IF EXISTS chk_incidents_status,
    DROP CONSTRAINT IF EXISTS chk_incidents_severity;
