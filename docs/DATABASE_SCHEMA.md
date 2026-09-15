# Database Schema & Domain Models Specification

## 1. Overview & Multi-Tenancy Architecture

criAIsis is a multi-tenant AI incident debate engine. Multi-tenancy is an architectural invariant enforced at both the PostgreSQL relational schema level and the Go repository data-access layer:

1. **Row-Level Tenant Scoping**: Every table downstream of `workspaces` contains an indexed `workspace_id UUID NOT NULL` column referencing `workspaces(id) ON DELETE CASCADE`.
2. **Compound Indexing & Unique Constraints**: Multi-column constraints pair `workspace_id` with domain keys (such as `(workspace_id, key)` for personas, and `(workspace_id, slack_channel_id, slack_thread_ts)` for incidents).
3. **High-Frequency Native Columns + JSONB Extension Slot**: All query-intensive, indexed, foreign-key, and filtering attributes exist as typed native columns. A `metadata JSONB NOT NULL DEFAULT '{}'` column is included on evolving entities (`document_chunks`, `incidents`, `debate_turns`) strictly as an extension slot for non-indexed contextual data (e.g. source line numbers, token usage counters), never as a replacement for relational columns.
4. **Hybrid Search Grounding**: `document_chunks` includes both an HNSW dense vector embedding (`vector(1536)`) and a stored sparse text search vector (`tsv tsvector`). Retrieval leverages Reciprocal Rank Fusion (RRF) to combine semantic similarity with exact error/symbol matching without external search engines.
5. **Token Encryption**: Slack OAuth bot tokens (`slack_bot_token_encrypted`) are stored as encrypted byte arrays (`BYTEA`) using AES-GCM-256 with a customer-isolated key derivation or master encryption secret.
6. **Defense-in-Depth Row Level Security (RLS)**: PostgreSQL native RLS policies enforce tenant boundaries at the database engine level via session variable `app.current_workspace_id`.

---

## 2. PostgreSQL 16 & pgvector DDL

```sql
-- Extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";

-- 1. Workspaces (Tenants / Slack Teams)
CREATE TABLE workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slack_team_id VARCHAR(64) UNIQUE NOT NULL,
    slack_team_name VARCHAR(255) NOT NULL,
    slack_bot_token_encrypted BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_workspaces_slack_team_id ON workspaces(slack_team_id);

-- 2. Users (Dashboard Operators & Workspace Members)
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    slack_user_id VARCHAR(64) NOT NULL,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'member', -- 'admin', 'member'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_workspace_slack_user UNIQUE (workspace_id, slack_user_id)
);

CREATE INDEX idx_users_workspace_id ON users(workspace_id);
CREATE INDEX idx_users_email ON users(workspace_id, email);

-- 3. Personas (Fixed 4 domain specialists per workspace)
CREATE TABLE personas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    key VARCHAR(32) NOT NULL, -- 'network', 'database', 'application', 'security'
    display_name VARCHAR(64) NOT NULL,
    system_prompt TEXT NOT NULL,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_workspace_persona_key UNIQUE (workspace_id, key)
);

CREATE INDEX idx_personas_workspace_id ON personas(workspace_id);

-- 4. Documents (Uploaded Runbooks and Knowledge Bases)
CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    persona_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    raw_content TEXT NOT NULL,
    content_hash VARCHAR(64) NOT NULL, -- SHA-256 hash for deduplication
    status VARCHAR(32) NOT NULL DEFAULT 'pending', -- 'pending', 'indexed', 'failed'
    chunk_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_documents_workspace_persona ON documents(workspace_id, persona_id);
CREATE INDEX idx_documents_workspace_status ON documents(workspace_id, status);
CREATE INDEX idx_documents_hash ON documents(workspace_id, content_hash);

-- 5. Document Chunks with Vector Embeddings & Hybrid Full-Text Search
CREATE TABLE document_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    persona_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL,
    chunk_text TEXT NOT NULL,
    token_count INT NOT NULL,
    embedding vector(1536) NOT NULL, -- OpenAI text-embedding-3-small or compatible
    tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', chunk_text)) STORED,
    metadata JSONB NOT NULL DEFAULT '{}', -- Extension slot: section headers, source lines, file paths
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Compound index for fast tenant and persona scoping
CREATE INDEX idx_chunks_scope ON document_chunks(workspace_id, persona_id, document_id);

-- Hierarchical Navigable Small World (HNSW) index for sub-50ms cosine similarity search
CREATE INDEX idx_chunks_embedding_hnsw ON document_chunks 
USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);

-- GIN index on tsvector for high-performance keyword matching
CREATE INDEX idx_chunks_tsv ON document_chunks USING gin(tsv);

-- 6. Incidents (Debate Sessions)
CREATE TABLE incidents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    slack_channel_id VARCHAR(64) NOT NULL,
    slack_thread_ts VARCHAR(64) NOT NULL,
    severity VARCHAR(32) NOT NULL DEFAULT 'sev-2', -- 'sev-1', 'sev-2', 'sev-3', 'sev-4'
    status VARCHAR(32) NOT NULL DEFAULT 'investigating', -- 'investigating', 'resolved'
    created_by_slack_user_id VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}', -- Extension slot: channel name, root-cause tags (Code/Infra/Hybrid)
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_workspace_channel_thread UNIQUE (workspace_id, slack_channel_id, slack_thread_ts)
);

CREATE INDEX idx_incidents_workspace_status ON incidents(workspace_id, status, created_at DESC);
CREATE INDEX idx_incidents_workspace_severity ON incidents(workspace_id, severity, created_at DESC);

-- 7. Debate Turns (Debate Messages & Excerpt Citations)
CREATE TABLE debate_turns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    persona_id UUID REFERENCES personas(id) ON DELETE SET NULL, -- NULL for Incident Commander synthesis
    stage INT NOT NULL, -- 1: Specialist Blast, 2: Cross-Rebuttal/Synthesis, 3: Follow-Up
    turn_type VARCHAR(32) NOT NULL, -- 'specialist_hypothesis', 'synthesis', 'follow_up'
    content TEXT NOT NULL,
    referenced_chunk_ids UUID[] NOT NULL DEFAULT '{}',
    slack_message_ts VARCHAR(64),
    metadata JSONB NOT NULL DEFAULT '{}', -- Extension slot: CitationSnapshot slice, model version, tokens, latency ms
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_debate_turns_incident_order ON debate_turns(incident_id, created_at ASC);
CREATE INDEX idx_debate_turns_workspace_created ON debate_turns(workspace_id, created_at DESC);

-- 8. Row Level Security (RLS) Policies (Defense-in-Depth)
ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE personas ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_chunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE debate_turns ENABLE ROW LEVEL SECURITY;

-- Dynamic tenant policy enforcing app.current_workspace_id
CREATE POLICY tenant_isolation_chunks ON document_chunks
FOR ALL USING (
    workspace_id = NULLIF(current_setting('app.current_workspace_id', true), '')::UUID
);

CREATE POLICY tenant_isolation_incidents ON incidents
FOR ALL USING (
    workspace_id = NULLIF(current_setting('app.current_workspace_id', true), '')::UUID
);

CREATE POLICY tenant_isolation_turns ON debate_turns
FOR ALL USING (
    workspace_id = NULLIF(current_setting('app.current_workspace_id', true), '')::UUID
);
```

---

## 3. Hybrid Search Query (Dense Vector + Full-Text RRF)

To maximize signal, retrieval combines dense vector similarity (semantic meaning) with PostgreSQL full-text search (exact error codes like `HTTP 504`, `PG-08006`, IP addresses) using Reciprocal Rank Fusion (RRF):

```sql
WITH vector_search AS (
    SELECT 
        id,
        ROW_NUMBER() OVER (ORDER BY embedding <=> $1::vector ASC) AS rank
    FROM document_chunks
    WHERE workspace_id = $2 AND persona_id = $3
    LIMIT $4 * 2
),
text_search AS (
    SELECT 
        id,
        ROW_NUMBER() OVER (ORDER BY ts_rank_cd(tsv, plainto_tsquery('english', $5)) DESC) AS rank
    FROM document_chunks
    WHERE workspace_id = $2 AND persona_id = $3
      AND tsv @@ plainto_tsquery('english', $5)
    LIMIT $4 * 2
)
SELECT 
    c.id,
    c.document_id,
    c.chunk_index,
    c.chunk_text,
    c.token_count,
    c.metadata,
    COALESCE(1.0 / (60 + v.rank), 0.0) + COALESCE(1.0 / (60 + t.rank), 0.0) AS rrf_score
FROM document_chunks c
LEFT JOIN vector_search v ON c.id = v.id
LEFT JOIN text_search t ON c.id = t.id
WHERE v.id IS NOT NULL OR t.id IS NOT NULL
ORDER BY rrf_score DESC
LIMIT $4;
```

---

## 4. Go Domain Models & Value Objects (Clean Architecture / SOLID)

In alignment with `solid-skills` and DDD, domain primitives are wrapped in typed Value Objects to eliminate primitive obsession. Domain entities expose behavior and protect invariants.

### 4.1 Value Objects (`internal/domain/value`)

```go
package value

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// WorkspaceID represents a strongly-typed workspace identifier.
type WorkspaceID struct {
	value uuid.UUID
}

func NewWorkspaceID() WorkspaceID {
	return WorkspaceID{value: uuid.New()}
}

func ParseWorkspaceID(raw string) (WorkspaceID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return WorkspaceID{}, fmt.Errorf("invalid workspace id: %w", err)
	}
	return WorkspaceID{value: parsed}, nil
}

func (w WorkspaceID) UUID() uuid.UUID { return w.value }
func (w WorkspaceID) String() string  { return w.value.String() }

// Slack identifiers
type SlackChannelID string
type SlackThreadTS string
type SlackUserID string

// PersonaKey defines the valid domain specialist persona keys.
type PersonaKey string

const (
	PersonaKeyNetwork     PersonaKey = "network"
	PersonaKeyDatabase    PersonaKey = "database"
	PersonaKeyApplication PersonaKey = "application"
	PersonaKeySecurity    PersonaKey = "security"
)

func ParsePersonaKey(raw string) (PersonaKey, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "network":
		return PersonaKeyNetwork, nil
	case "database":
		return PersonaKeyDatabase, nil
	case "application":
		return PersonaKeyApplication, nil
	case "security":
		return PersonaKeySecurity, nil
	default:
		return "", fmt.Errorf("invalid persona key '%s': must be network, database, application, or security", raw)
	}
}

func (p PersonaKey) String() string { return string(p) }

// IncidentStatus represents the lifecycle state of an incident.
type IncidentStatus string

const (
	IncidentStatusInvestigating IncidentStatus = "investigating"
	IncidentStatusResolved      IncidentStatus = "resolved"
)

func ParseIncidentStatus(raw string) (IncidentStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "investigating":
		return IncidentStatusInvestigating, nil
	case "resolved":
		return IncidentStatusResolved, nil
	default:
		return "", fmt.Errorf("invalid incident status: %s", raw)
	}
}

// Stage represents the clash debate phase.
type Stage int

const (
	StageSpecialistBlast     Stage = 1
	StageConsensusSynthesis Stage = 2
	StageInteractiveFollowUp Stage = 3
)

func ParseStage(s int) (Stage, error) {
	if s < 1 || s > 3 {
		return 0, fmt.Errorf("invalid clash stage: %d (must be 1, 2, or 3)", s)
	}
	return Stage(s), nil
}

// TurnType defines whether a turn is specialist, synthesis, or follow-up.
type TurnType string

const (
	TurnTypeSpecialistHypothesis TurnType = "specialist_hypothesis"
	TurnTypeSynthesis            TurnType = "synthesis"
	TurnTypeFollowUp             TurnType = "follow_up"
)

// Severity classifies the operational impact of an incident (sev-1 through sev-4).
type Severity string

const (
	SeveritySev1 Severity = "sev-1"
	SeveritySev2 Severity = "sev-2"
	SeveritySev3 Severity = "sev-3"
	SeveritySev4 Severity = "sev-4"
)

// DocumentStatus tracks the asynchronous ingestion lifecycle of runbooks.
type DocumentStatus string

const (
	DocumentStatusPending DocumentStatus = "pending"
	DocumentStatusIndexed DocumentStatus = "indexed"
	DocumentStatusFailed  DocumentStatus = "failed"
)

// CitationSnapshot preserves immutable runbook evidence inside DebateTurn metadata JSONB.
type CitationSnapshot struct {
	ChunkID       uuid.UUID `json:"chunk_id"`
	DocumentTitle string    `json:"document_title"`
	Snippet       string    `json:"snippet"`
	Source        string    `json:"source,omitempty"`
}

// EmbeddingVector represents a 1536-dimensional vector embedding.
type EmbeddingVector []float32

func NewEmbeddingVector(vals []float32) (EmbeddingVector, error) {
	if len(vals) != 1536 {
		return nil, fmt.Errorf("invalid embedding dimensions: expected 1536, got %d", len(vals))
	}
	return EmbeddingVector(vals), nil
}
```

### 4.2 Domain Entities (`internal/domain/entity`)

```go
package entity

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"criaisis/internal/domain/value"
)

type Workspace struct {
	id                     value.WorkspaceID
	slackTeamID            string
	slackTeamName          string
	slackBotTokenEncrypted []byte
	createdAt              time.Time
	updatedAt              time.Time
}

type Persona struct {
	id           uuid.UUID
	workspaceID  value.WorkspaceID
	key          value.PersonaKey
	displayName  string
	systemPrompt string
	isEnabled    bool
	createdAt    time.Time
	updatedAt    time.Time
}

type Document struct {
	id          uuid.UUID
	workspaceID value.WorkspaceID
	personaID   uuid.UUID
	title       string
	rawContent  string
	contentHash string
	status      value.DocumentStatus
	chunkCount  int
	createdAt   time.Time
	updatedAt   time.Time
}

type DocumentChunk struct {
	id          uuid.UUID
	workspaceID value.WorkspaceID
	personaID   uuid.UUID
	documentID  uuid.UUID
	chunkIndex  int
	chunkText   string
	tokenCount  int
	embedding   value.EmbeddingVector
	metadata    json.RawMessage // JSONB extension slot
	createdAt   time.Time
}

type Incident struct {
	id                 uuid.UUID
	workspaceID        value.WorkspaceID
	title              string
	description        string
	slackChannelID     value.SlackChannelID
	slackThreadTS      value.SlackThreadTS
	severity           value.Severity
	status             value.IncidentStatus
	createdBySlackUser value.SlackUserID
	metadata           json.RawMessage // JSONB extension slot: root-cause classification (Code/Infra/Hybrid)
	resolvedAt         *time.Time
	createdAt          time.Time
	updatedAt          time.Time
}

func (i *Incident) Resolve(resolvedAt time.Time) error {
	if i.status == value.IncidentStatusResolved {
		return errors.New("incident already resolved")
	}
	i.status = value.IncidentStatusResolved
	i.resolvedAt = &resolvedAt
	i.updatedAt = resolvedAt
	return nil
}

type DebateTurn struct {
	id                 uuid.UUID
	incidentID         uuid.UUID
	workspaceID        value.WorkspaceID
	personaID          *uuid.UUID
	stage              value.Stage
	turnType           value.TurnType
	content            string
	referencedChunkIDs []uuid.UUID
	slackMessageTS     string
	metadata           json.RawMessage // JSONB extension slot: token counts, latency ms
	createdAt          time.Time
}
```

### 4.3 Repository Contracts & Transaction Management (`internal/domain/repository`)

```go
package repository

import (
	"context"

	"github.com/google/uuid"
	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
)

// TransactionManager provides atomic transaction boundaries across repository operations.
type TransactionManager interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type WorkspaceRepository interface {
	Create(ctx context.Context, ws *entity.Workspace) error
	GetByID(ctx context.Context, id value.WorkspaceID) (*entity.Workspace, error)
	GetBySlackTeamID(ctx context.Context, teamID string) (*entity.Workspace, error)
	Update(ctx context.Context, ws *entity.Workspace) error
}

type PersonaRepository interface {
	ListByWorkspace(ctx context.Context, wsID value.WorkspaceID) ([]*entity.Persona, error)
	GetByKey(ctx context.Context, wsID value.WorkspaceID, key value.PersonaKey) (*entity.Persona, error)
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Persona, error)
	Update(ctx context.Context, persona *entity.Persona) error
}

type DocumentRepository interface {
	Create(ctx context.Context, doc *entity.Document) error
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Document, error)
	ListByPersona(ctx context.Context, wsID value.WorkspaceID, personaID uuid.UUID) ([]*entity.Document, error)
	Delete(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) error
}

type ScopedChunkResult struct {
	Chunk           *entity.DocumentChunk
	SimilarityScore float64
}

type ChunkRepository interface {
	CreateBatch(ctx context.Context, chunks []*entity.DocumentChunk) error
	DeleteByDocumentID(ctx context.Context, wsID value.WorkspaceID, docID uuid.UUID) error
	SearchHybrid(
		ctx context.Context, 
		wsID value.WorkspaceID, 
		personaID uuid.UUID, 
		queryEmbedding value.EmbeddingVector, 
		queryText string,
		topK int,
	) ([]ScopedChunkResult, error)
}

type IncidentRepository interface {
	Create(ctx context.Context, inc *entity.Incident) error
	GetByID(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) (*entity.Incident, error)
	GetBySlackThread(ctx context.Context, wsID value.WorkspaceID, channelID value.SlackChannelID, threadTS value.SlackThreadTS) (*entity.Incident, error)
	List(ctx context.Context, wsID value.WorkspaceID, limit int, offset int) ([]*entity.Incident, error)
	Resolve(ctx context.Context, wsID value.WorkspaceID, id uuid.UUID) error
}

type DebateTurnRepository interface {
	Create(ctx context.Context, turn *entity.DebateTurn) error
	ListByIncident(ctx context.Context, wsID value.WorkspaceID, incidentID uuid.UUID) ([]*entity.DebateTurn, error)
}
```
