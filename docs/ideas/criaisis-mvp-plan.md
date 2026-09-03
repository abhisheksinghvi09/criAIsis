# AI War Room (criAIsis) — Idea Refinement & MVP One-Pager

## Problem Statement
How might we empower an on-call incident commander to rapidly eliminate cross-domain uncertainty during an active incident by orchestrating a grounded, multi-agent debate among specialist AI personas directly inside Slack within 25 seconds?

## Recommended Direction: 2-Stage Parallel Clash Engine (Hybrid Slack + Lean UI)
Build a **multi-tenant, 2-stage asynchronous incident debate engine** using **Go + PostgreSQL 16 (pgvector)**. 

During an incident, 4 domain specialists (Network, Database, Application, Security) run concurrent RAG queries against their respective runbook documentation and formulate hypotheses in parallel (<10s). An orchestrator then conducts an adversarial cross-rebuttal and consensus synthesis (<15s) that highlights contradictions (e.g. database locks vs. upstream packet drops) and isolates the most probable root cause with cited runbook sources.

All live operational interaction happens inside Slack threads (slash commands, threaded turns, `@mention` follow-ups, resolution). A lean Vite/React dashboard serves strictly for runbook management and audit transcript viewing—cutting out all vanity analytics and billing clutter for Day-1.

Multi-tenancy is enforced from Day 1 at the database schema and query repository level (`workspace_id` foreign keys and tenant-isolated vector filtering), ensuring seamless transition from internal dogfooding to external B2B SaaS without rewrites.

## Key Assumptions to Validate
- [ ] **Cross-Domain Signal:** Specialist agents debating each other generate higher diagnostic signal and lower hallucination than a single flat LLM summary. (Validate with 5 real past post-mortems).
- [ ] **Latency Ceiling:** End-to-end Slack thread turnaround remains under 25 seconds during an active incident.
- [ ] **Citation Grounding:** Specialist agents cite specific runbook excerpts and fail visibly if documentation is absent rather than guessing.
- [ ] **Lean Concurrency:** In-process Go worker channels (`chan IncidentJob`) provide rock-solid queuing without the operational burden of a Redis cluster for MVP volume.

## MVP Scope
- **Slack Bot:** Slack OAuth 2.0 install, `/criaisis debate <description>` slash command, threaded turn responses, `@criaisis @<persona>` follow-up routing, `/criaisis resolve`.
- **Debate Architecture:** 2-Stage Parallel Clash (Stage 1: 4 concurrent specialist hypotheses; Stage 2: Cross-examination and consensus summary).
- **Specialists:** 4 fixed personas (Network, Database, Application, Security) with persona-isolated RAG.
- **Backend:** Single Go binary with in-memory worker pool, PostgreSQL 16 + `pgvector`.
- **Web Dashboard:** Slack OAuth login, runbook upload & persona tagging, persona prompt inspector, transcript viewer with cited excerpts.

## Not Doing (and Why)
- **Redis Cluster:** Go's in-memory buffered worker channels satisfy concurrency and fast webhook acknowledgments without external infrastructure. Swappable via `JobQueue` interface later.
- **Analytics & Vanity Metrics:** MTTR, incidents-per-week, and document usage charts add development cost without helping an engineer fix a broken database at 2:00 AM.
- **Live Monitoring Integrations (Datadog/PagerDuty/K8s):** High API surface and maintenance. MVP proves value on runbooks and incident descriptions first.
- **Custom User-Defined Personas:** Restricting to 4 core infrastructure domains prevents prompt complexity and unbounded debate rounds.
- **Self-Serve Billing / Stripe:** Free pilot tier hardcoded for early customers and dogfooding.

## Open Questions
- What chunking strategy preserves tabular runbook steps best (Markdown headers vs recursive tokens)?
- Should specialists output structured JSON to the orchestrator before formatting into Slack Block Kit markdown?

