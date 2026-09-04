# criAIsis: MVP Product & Engineering Plan

## 1. Problem Statement
> **How might we** empower an on-call incident commander to rapidly eliminate cross-domain uncertainty during an active incident by orchestrating a grounded, multi-agent debate among specialist AI personas directly inside Slack within 25 seconds?

---

## 2. Product Philosophy: Investigative Detective, Never Autonomous Remediation

A foundational boundary of criAIsis is:
> **The Agent's Role is Deep Read-Only Diagnostic Investigation, NEVER Blind Autonomous Production Remediation.**

The agent is an **investigative detective**, not a reckless operator. It does not touch production buttons, modify configurations, or execute destructive mutations. Instead, with optional read-only permissions (via APIs or Model Context Protocol / MCP), it rapidly answers the three core questions incident commanders spend 45 minutes investigating:
1. **Which specific logs and error stack traces are failing?**
2. **Which department/team owns the problem?** (Network vs. Database vs. Application vs. Security)
3. **What is the root-cause classification?** Is it an **Infrastructure issue**, a **Code-level regression**, or **Both (Hybrid)**?

### The Three Failure Modes criAIsis Classifies
- **Code-Level Issue:** A new release introduced an unhandled exception, syntax error, or breaking API payload change.
- **Infrastructure Issue:** Cloud availability zone impairment, node disk saturation, network packet drops, or hardware failure.
- **Hybrid (Code + Infra):** A new code release introduced an un-indexed query or memory leak that saturated database connection pools or caused container OOM crashloops.

---

## 3. Recommended Direction: The 2-Stage Asynchronous Clash

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

1. **Stage 1 — Parallel Hypotheses (<10s):** The 4 domain specialist agents (Network, Database, Application, Security) execute RAG retrieval against their respective runbook chunks concurrently via Go goroutines, formulating initial domain assessments.
2. **Stage 2 — Cross-Rebuttal & IC Consensus Synthesis (<15s):** An orchestrator agent cross-examines the Stage 1 outputs, highlights contradictions (e.g., "DB claims deadlock, but Network shows 90% packet drops upstream"), and produces a synthesized Incident Commander (IC) hypothesis with cited document excerpts and root-cause classification (Code, Infra, or Hybrid).
3. **Stage 3 (Interactive Follow-up):** The IC can `@mention` any specific persona in the Slack thread (`@criaisis @database why are connections spiking?`) for targeted deep dives. The agent provides runbook-grounded diagnostics and exact copy-paste diagnostic queries for human execution.

---

## 4. Architecture Principles & Anti-Overengineering Guardrails

| Principle | Decision | Rationale |
|---|---|---|
| **Multi-Tenancy from Day 1** | Schema-level `workspace_id` scoping on every table + Repository-level filtering | Ensures zero data leakage between customer Slack workspaces. Enforced at compile time via typed `value.WorkspaceID`. |
| **Lean Concurrency** | Go worker channels (`chan IncidentJob`) fulfilling a clean `JobQueue` interface | Removes Redis cluster deployment and operational moving parts for MVP, while preserving zero-cost migration to Redis when scaling. |
| **Unified Data Store** | PostgreSQL 16 with `pgvector` | Relational data (workspaces, personas, incidents, transcripts) and vector embeddings (`vector(1536)`) reside in one database with HNSW and GIN hybrid indexing. |
| **Hybrid UI Surface** | Slack for live incidents; Lean React dashboard strictly for Runbooks & Transcripts | Live firefighting stays where engineers already are (Slack). The web dashboard is focused on runbook uploads and audit transcript reviews. |
| **Micro-Batch Ingestion** | Explicit document ingestion lifecycle (`pending`, `indexed`, `failed`) | Balances near real-time ingestion with resource efficiency, preventing UI timeouts during runbook chunking. |
| **Hybrid Citations** | Relational IDs (`UUID[]`) + Immutable Snapshots in JSONB | Normalized joins for fast queries, plus permanent citation text preservation in transcripts surviving document deletion. |

---

## 5. MVP Scope Definition

### In-Scope (MVP)
* **Slack App (Bot + Slash Commands):**
  * OAuth 2.0 install flow with encrypted bot token storage (AES-GCM-256).
  * `/criaisis investigate <incident description>`: Acknowledges in <500ms, enqueues debate, posts in thread.
  * 2-Stage parallel debate output formatted with Slack Block Kit.
  * In-thread `@mention` follow-up routing to specific specialist personas.
  * `/criaisis resolve`: Marks incident resolved, locks transcript, timestamps resolution.
* **Specialist Personas (4 Fixed):**
  * `network`: Routing, DNS, load balancers, CDN, egress/ingress.
  * `database`: Connection pools, query locks, replication lag, I/O bottlenecks.
  * `application`: Deployments, memory leaks, unhandled panics, downstream microservices.
  * `security`: DDoS, credential stuffing, cert expirations, IAM anomalies.
* **Persona-Scoped Hybrid RAG Pipeline:**
  * Document ingestion (Markdown / Plain text) with status tracking (`pending`, `indexed`, `failed`).
  * Chunking (recursive character splitter, ~500 tokens, 10% overlap).
  * Embeddings generation (`text-embedding-3-small` / OpenAI or compatible).
  * Hybrid search (HNSW cosine similarity + `tsvector` full-text search with Reciprocal Rank Fusion) strictly scoped by `workspace_id` AND `persona_id`.
* **Lean Web Dashboard (Vite + React + Tailwind):**
  * Slack OAuth sign-in.
  * Document management: upload markdown files, assign to persona, view chunk counts and status, delete.
  * Persona prompt viewer: review and update system prompts for each agent.
  * Incident History & Transcript Viewer: inspect turns, severity tiers, and cited runbook source chunks.

### Out of Scope (Ruthlessly Cut for MVP)
* [-] **No Autonomous Remediation:** Agent never applies mutations or restarts pods in production.
* [-] **No Redis Cluster:** In-process Go worker pool handles MVP job queuing.
* [-] **No Vanity Analytics Charts:** No MTTR, incidents/week, or document usage charts.
* [-] **No Custom Personas:** Exactly 4 fixed domain personas.
* [-] **No Self-Serve Stripe Billing:** Free/pilot tier hardcoded.

---

## 6. Dev Environment Incident Simulation & Debugging Framework

To verify the multi-agent clash and diagnostic classification without requiring a live production outage, criAIsis includes a **Local Incident Simulation Framework**:

```
┌────────────────────────────────────────────────────────────────────────┐
│               Local Dev Incident Simulation Framework                  │
├────────────────────────────────┬───────────────────────────────────────┤
│ Scenario Fixtures              │ Execution Mode                        │
│ - checkout_spike.json          │ Taskfile Command:                     │
│ - db_connection_exhaustion.json│ $ task incident:simulate -- scenario= │
│ - pod_oom_kill.json            │   db_connection_exhaustion            │
├────────────────────────────────┴───────────────────────────────────────┤
│ Verification Pipeline:                                                 │
│ 1. Seeds local Docker pgvector with scenario runbooks                 │
│ 2. Fires simulated slash command via CLI / HTTP handler                │
│ 3. Asserts Stage 1 specialist hypotheses generate valid citations      │
│ 4. Asserts Stage 2 synthesizer detects correct root-cause (Code/Infra) │
│ 5. Validates thread persistence in local debate_turns table            │
└────────────────────────────────────────────────────────────────────────┘
```

This simulation framework allows developers to:
1. Reproduce and step through complex multi-agent debates with deterministic mock inputs.
2. Benchmark synthesis prompt accuracy across various failure scenarios (pure infra, pure code, hybrid).
3. Debug prompt regressions and test new LLM provider integrations locally.

---

## 7. Key Assumptions to Validate
- [ ] **Signal Quality:** 4 specialist prompts + RAG excerpts produce a sharper hypothesis than a single mega-prompt (Test with 5 benchmark incidents).
- [ ] **Debate Latency:** Total debate turnaround stays under 25 seconds end-to-end on Slack.
- [ ] **Runbook Grounding:** Personas cite specific runbook sections rather than hallucinating generic advice.
- [ ] **Classification Accuracy:** Synthesizer correctly categorizes incidents into Code, Infra, or Hybrid root causes.
