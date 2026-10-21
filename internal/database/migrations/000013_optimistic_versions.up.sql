-- Optimistic concurrency control. Each update must name the version it read and
-- bumps it; a writer holding a stale copy matches no row and retries on fresh
-- data instead of silently overwriting a concurrent change.
ALTER TABLE incidents          ADD COLUMN version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE workspaces         ADD COLUMN version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE workspace_settings ADD COLUMN version BIGINT NOT NULL DEFAULT 0;
