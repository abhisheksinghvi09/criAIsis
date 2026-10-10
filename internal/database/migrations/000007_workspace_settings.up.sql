-- Bring-your-own-key tenancy.
--
-- Every customer supplies their own model credentials and their own chat
-- destination. Those live here rather than in process configuration, so one
-- tenant's spend, rate limits and blast radius are entirely their own, and the
-- platform operator never holds a shared key that all tenants ride on.
--
-- Secrets are stored only as AES-GCM-256 ciphertext. Nothing in this table is
-- readable without CRIAISIS_CREDENTIAL_ENCRYPTION_KEY.

CREATE TABLE workspace_settings (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,

    -- The debate (Stage 1 and Stage 2)
    llm_provider          VARCHAR(16) NOT NULL DEFAULT 'anthropic',
    llm_api_key_encrypted BYTEA,
    llm_specialist_model  VARCHAR(64) NOT NULL DEFAULT 'claude-opus-5',
    llm_synthesis_model   VARCHAR(64) NOT NULL DEFAULT 'claude-opus-5',

    -- Retrieval. Kept separate because Anthropic exposes no embeddings endpoint,
    -- so this is always a different vendor and a different key.
    embedding_provider          VARCHAR(16)  NOT NULL DEFAULT 'openai',
    embedding_api_key_encrypted BYTEA,
    embedding_model             VARCHAR(64)  NOT NULL DEFAULT 'text-embedding-3-small',
    embedding_base_url          VARCHAR(255) NOT NULL DEFAULT 'https://api.openai.com/v1',

    -- Where finished investigations are posted
    notify_provider              VARCHAR(16) NOT NULL DEFAULT 'none',
    notify_webhook_url_encrypted BYTEA,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT chk_settings_llm_provider       CHECK (llm_provider IN ('anthropic')),
    CONSTRAINT chk_settings_embedding_provider CHECK (embedding_provider IN ('openai')),
    CONSTRAINT chk_settings_notify_provider    CHECK (notify_provider IN ('none', 'slack', 'discord')),
    -- A configured destination without a URL would fail silently at delivery time.
    CONSTRAINT chk_settings_notify_url CHECK (
        notify_provider = 'none' OR notify_webhook_url_encrypted IS NOT NULL
    )
);

CREATE TRIGGER set_updated_at_workspace_settings
    BEFORE UPDATE ON workspace_settings
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();

-- Move the notification destination out of workspaces: that table is tenancy and
-- identity, this one is integration credentials.
INSERT INTO workspace_settings (workspace_id, notify_provider, notify_webhook_url_encrypted)
SELECT id, notify_provider, notify_webhook_url_encrypted FROM workspaces
ON CONFLICT (workspace_id) DO NOTHING;

ALTER TABLE workspaces DROP CONSTRAINT IF EXISTS chk_workspaces_notify_provider;
ALTER TABLE workspaces DROP COLUMN notify_webhook_url_encrypted;
ALTER TABLE workspaces DROP COLUMN notify_provider;

-- Administrative credential for the management API. Separate from the alert
-- ingestion token so a monitoring system that can open incidents cannot also
-- read or rewrite the tenant's API keys.
ALTER TABLE workspaces ADD COLUMN admin_api_key_hash BYTEA;
