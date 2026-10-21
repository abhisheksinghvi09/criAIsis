ALTER TABLE workspace_settings DROP COLUMN IF EXISTS version;
ALTER TABLE workspaces         DROP COLUMN IF EXISTS version;
ALTER TABLE incidents          DROP COLUMN IF EXISTS version;
