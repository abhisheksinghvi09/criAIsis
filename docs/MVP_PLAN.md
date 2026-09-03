# criAIsis: MVP Product & Engineering Plan

## 1. Problem Statement
> **How might we** empower an on-call incident commander to rapidly eliminate cross-domain uncertainty during an active incident by orchestrating a grounded, multi-agent debate among specialist AI personas directly inside Slack within 25 seconds?

---

## 2. Recommended Direction: The 2-Stage Asynchronous Clash

Rather than running slow, sequential turn-by-turn debate rounds (which take 60–90+ seconds over 8–12 LLM roundtrips), criAIsis adopts a **2-Stage Asynchronous Clash Model**:

```
                         ┌───────────────────────────────────────────────────┐
                         │              The 2-Stage Clash Model               │
                         └───────────────────────────────────────────────────┘
                                                  │
                                                  ▼
   [Stage 1: Parallel Blast (~6-8s)]                             [Stage 2: Cross-Rebuttal (~8-10s)]
  ┌─────────────────────────────────┐                           ┌──────────────────────────────────┐
  │ Network Agent (RAG: Net docs)   │───┐                       │ Cross-Domain Synthesis           │
  ├─────────────────────────────────┤   │                       │                                  │
  │ DB Agent      (RAG: DB docs)    │───┼──► [Transcript Log] ──► "DB Agent suspects locks, but   │
  ├─────────────────────────────────┤   │                       │  Network Agent shows 80% packet  │
  │ App Agent     (RAG: App docs)   │───┤                       │  drop between AZ-a and AZ-b.     │
  ├─────────────────────────────────┤   │                       │  Root Cause: Upstream Gateway."  │
  │ Security Agent(RAG: Sec docs)   │───┘                       └──────────────────────────────────┘
  └─────────────────────────────────┘                                            │
                                                                                 ▼
                                                                  [Slack Thread + IC Summary]
```

1. **Stage 1 — Parallel Hypotheses (<10s):** The 4 domain specialist agents (Network, Database, Application, Security) execute RAG retrieval against their respective runbook chunks concurrently via Go goroutines, posting their initial domain assessments.
2. **Stage 2 — Cross-Rebuttal & IC Consensus Synthesis (<15s):** An orchestrator agent cross-examines the Stage 1 outputs, highlights contradictions (e.g., "DB claims deadlock, but Network shows 90% packet drops upstream"), and produces a crisp, synthesized Incident Commander (IC) hypothesis with cited document excerpts.
3. **Stage 3 (Interactive Follow-up):** The IC can `@mention` any specific persona in the Slack thread (`@criaisis @db what about pg_stat_activity?`) for targeted deep dives.

---

## 3. Architecture Principles & Anti-Overengineering Guardrails

| Principle | Decision | Rationale |
|---|---|---|
| **Multi-Tenancy from Day 1** | Schema-level `workspace_id` scoping on every table + Repository-level filtering | Ensures zero data leakage between customer Slack workspaces. Built right initially so expanding from internal dogfooding to B2B SaaS requires no rewrites. |
| **Lean Concurrency** | Go worker channels (`chan IncidentJob`) fulfilling a clean `JobQueue` interface | Removes Redis cluster deployment, connection pools, and extra operational moving parts for MVP, while preserving zero-cost migration to Redis when horizontally scaling later. |
| **Unified Data Store** | PostgreSQL 16 with `pgvector` | Relational data (workspaces, personas, incidents, transcripts) and vector embeddings (`vector(1536)`) reside in one database. Single backup, single migration runner. |
| **Hybrid UI Surface** | Slack for live incidents; Lean React dashboard strictly for Runbooks & Transcripts | Live firefighting stays where engineers already are (Slack). The web dashboard is stripped down to runbook upload and audit transcript viewing—cutting out vanity analytics. |

---

## 4. MVP Scope Definition

### In-Scope (MVP)
* **Slack App (Bot + Slash Commands):**
  * OAuth 2.0 install flow with encrypted bot token storage.
  * `/criaisis debate <incident description>`: Acknowledges in <500ms, enqueues debate, posts in thread.
  * 2-Stage parallel debate output formatted with Slack Block Kit.
  * In-thread `@mention` follow-up routing to specific specialist personas.
  * `/criaisis resolve`: Marks incident resolved, locks transcript, timestamps resolution.
* **Specialist Personas (4 Fixed):**
  * `network`: Routing, DNS, load balancers, CDN, egress/ingress.
  * `database`: Connection pools, query locks, replication lag, I/O bottlenecks.
  * `application`: Deployments, memory leaks, unhandled panics, downstream microservices.
  * `security`: DDoS, credential stuffing, cert expirations, IAM anomalies.
* **Persona-Scoped RAG Pipeline:**
  * Document ingestion (Markdown / Plain text).
  * Chunking (recursive character splitter, ~500 tokens, 10% overlap).
  * Embeddings generation (`text-embedding-3-small` / OpenAI or compatible).
  * Cosine similarity search strictly scoped by `workspace_id` AND `persona_id`.
* **Lean Web Dashboard (Vite + React + Tailwind):**
  * Slack OAuth sign-in.
  * Document management: upload markdown files, assign to persona, view chunk counts, delete.
  * Persona prompt viewer: review system prompts for each agent.
  * Incident History & Transcript Viewer: inspect turns and cited runbook source chunks.

### Out of Scope (Ruthlessly Cut for MVP)
* [-] **No Redis Cluster:** In-process Go worker pool handles MVP job queuing.
* [-] **No Analytics Charts:** No MTTR, incidents/week, or document usage analytics.
* [-] **No Live Telemetry Ingestion:** No direct Datadog, Prometheus, or PagerDuty webhooks for Day-1.
* [-] **No Custom Personas:** Exactly 4 fixed domain personas.
* [-] **No Self-Serve Stripe Billing:** Free/pilot tier hardcoded.

---

## 5. Multi-Tenant Data Model (PostgreSQL + pgvector)

```sql
-- Workspaces (Slack Teams)
CREATE TABLE workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slack_team_id VARCHAR(64) UNIQUE NOT NULL,
    slack_team_name VARCHAR(255) NOT NULL,
    slack_bot_token_encrypted BYTEA NOT NULL, -- AES-GCM-256
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Personas (Fixed 4 per workspace)
CREATE TABLE personas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    key VARCHAR(32) NOT NULL, -- 'network', 'database', 'application', 'security'
    display_name VARCHAR(64) NOT NULL,
    system_prompt TEXT NOT NULL,
    is_enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(workspace_id, key)
);

-- Documents (Uploaded runbooks)
CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    persona_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    raw_content TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Document Chunks with Vector Embeddings
CREATE TABLE document_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    persona_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL,
    chunk_text TEXT NOT NULL,
    embedding vector(1536) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_chunks_retrieval ON document_chunks 
USING hnsw (embedding vector_cosine_ops)
WHERE workspace_id IS NOT NULL;

-- Incidents
CREATE TABLE incidents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    slack_channel_id VARCHAR(64) NOT NULL,
    slack_thread_ts VARCHAR(64) NOT NULL,
    status VARCHAR(32) DEFAULT 'investigating', -- 'investigating', 'resolved'
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Debate Turns & Citations
CREATE TABLE debate_turns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    persona_id UUID REFERENCES personas(id) ON DELETE SET NULL, -- NULL for IC synthesis
    stage INT NOT NULL, -- 1 = Specialist, 2 = Cross-rebuttal/Synthesis, 3 = Follow-up
    turn_type VARCHAR(32) NOT NULL, -- 'specialist_hypothesis', 'synthesis', 'follow_up'
    content TEXT NOT NULL,
    referenced_chunk_ids UUID[] DEFAULT '{}',
    slack_message_ts VARCHAR(64),
    created_at TIMESTAMPTZ DEFAULT NOW()
);
```

---

## 6. Agile Delivery Plan: 4 Sprints

### Sprint 1: Multi-Tenant Core & Ingestion Engine (1 Week)
* **User Story 1.1:** As a system, I need a PostgreSQL database with `pgvector` that strictly isolates workspace data.
  * *Acceptance Criteria:* DB migrations execute cleanly; composite keys and `workspace_id` foreign keys prevent cross-tenant queries; vector indices allow <50ms similarity search.
* **User Story 1.2:** As an Admin, I can upload a markdown runbook tagged to a persona, and the system automatically chunks and embeds it.
  * *Acceptance Criteria:* Document is parsed into ~500-token chunks with 10% overlap; embeddings generated and stored; persona-scoped retrieval returns top-K chunks without leaking other persona documents.

### Sprint 2: 2-Stage Clash Orchestrator & Slack Integration (1 Week)
* **User Story 2.1:** As an Incident Commander, I can type `/criaisis debate <description>` in Slack and receive immediate acknowledgement.
  * *Acceptance Criteria:* Slash command verifies Slack signature and responds HTTP 200 with an ephemeral message in <500ms; debate job enqueued to worker pool.
* **User Story 2.2:** As an Incident Commander, I see 4 specialist hypotheses posted in-thread followed by an adversarial consensus summary in <25 seconds.
  * *Acceptance Criteria:* Go worker spawns 4 goroutines with `errgroup` for Stage 1; Stage 2 orchestrator feeds Stage 1 outputs into synthesis prompt; results formatted as Slack Block Kit messages with cited runbook tags.

### Sprint 3: Slack Follow-ups & Lean Dashboard (1 Week)
* **User Story 3.1:** As an Incident Commander, I can ask follow-up questions to specific personas via `@mention`.
  * *Acceptance Criteria:* Slack event listener parses `@criaisis @database <question>`, retrieves prior thread context + DB runbook chunks, and posts grounded reply.
* **User Story 3.2:** As an Admin, I can view past incident transcripts and upload runbooks through a minimal React dashboard.
  * *Acceptance Criteria:* Slack OAuth login; markdown drag-and-drop document uploader with persona dropdown; transcript viewer showing full turns and cited excerpts.

### Sprint 4: Security Hardening, Verification & Live Fire Drill (1 Week)
* **User Story 4.1:** As an Admin, all sensitive tokens and customer runbooks are encrypted and audited.
  * *Acceptance Criteria:* Slack bot tokens encrypted with AES-GCM-256; graceful degradation implemented (if one agent LLM call fails, remaining 3 agents still post).
* **User Story 4.2:** As an Engineering Team, we run a simulated Sev-1 fire drill (e.g., simulated DB pool exhaustion) to benchmark latency, accuracy, and signal-to-noise ratio.
  * *Acceptance Criteria:* End-to-end debate completes in under 25s; root-cause hypothesis matches simulated failure.

---

## 7. Key Assumptions to Validate
- [ ] **Signal Quality:** 4 specialist prompts + RAG excerpts produce a sharper hypothesis than a single mega-prompt (Test with 5 benchmark incidents).
- [ ] **Debate Latency:** Total debate turnaround stays under 25 seconds end-to-end on Slack.
- [ ] **Runbook Grounding:** Personas cite specific runbook sections rather than hallucinating generic advice.

