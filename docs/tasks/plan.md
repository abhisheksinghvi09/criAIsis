# Implementation Plan: Database Schema & Domain Model Foundation

## Overview
criAIsis is a multi-tenant AI incident debate engine running inside Slack threads, powered by a 2-Stage Asynchronous Clash among 4 specialist AI personas (Network, Database, Application, Security) grounded in runbook documents. 

**Core Product Mandate:**
> **The Agent's Role is Deep Read-Only Diagnostic Investigation, NEVER Blind Autonomous Production Remediation.**
> The agents act as expert investigative detectives. They do not mutate production or execute destructive operations. Instead, via grounded RAG and optional read-only diagnostic APIs (or Model Context Protocol / MCP), they rapidly answer:
> 1. Which specific logs and error stack traces are failing?
> 2. Which department/domain owns the problem? (Network, Database, Application, Security)
> 3. What is the root cause classification? (Infrastructure, Code-level, or Both/Hybrid)

---

## Architecture Decisions

- **PostgreSQL 16 with pgvector**: Relational entities (`workspaces`, `users`, `personas`, `incidents`, `debate_turns`) and vector embeddings (`vector(1536)`) reside in a single database. This eliminates dual-write drift and removes the operational overhead of a standalone vector database.
- **Multi-Tenant Scoping at Schema & Repo Layers**: Every table downstream of `workspaces` contains `workspace_id UUID NOT NULL` with foreign key cascades. Every repository method takes `workspace_id` and enforces it in the SQL `WHERE` clause.
- **HNSW Cosine Vector Index**: `document_chunks` index uses HNSW with `vector_cosine_ops`, `m = 16`, and `ef_construction = 64` for sub-50ms nearest neighbor search.
- **Clean Architecture & Domain Value Objects**: Domain models use strongly typed value objects (`WorkspaceID`, `PersonaKey`, `Severity`, `DocumentStatus`, `IncidentStatus`, `Stage`, `TurnType`, `EmbeddingVector`, `CitationSnapshot`) rather than raw primitives, enforcing invariants on creation.
- **Embedded Go Migration Engine**: Embed SQL migration files (`//go:embed`) and execute via `golang-migrate` with the native `iofs` driver to guarantee zero schema drift on boot.
- **Incident Severity Tiers**: Native 4-tier severity model (`sev-1` through `sev-4`) indexed for fast multi-tenant filtering.
- **Micro-Batch Document Ingestion**: Explicit ingestion status (`pending`, `indexed`, `failed`) and chunk counts to support non-blocking background workers.
- **Hybrid Citations**: Relational `referenced_chunk_ids UUID[]` plus immutable `CitationSnapshot` in `metadata JSONB` for permanent post-mortem auditability.
- **Automated Alert Ingestion**: Dedicated webhook endpoints (`POST /api/v1/integrations/alerts/:provider`) parsing Grafana and CloudWatch alert payloads into structured `IncidentContext` value objects.
- **Read-Only Diagnostic Tool Adapters (MCP)**: Pluggable Model Context Protocol interface contracts enabling specialist personas to query metrics (Prometheus, CloudWatch) with strict 5s timeouts and zero write mutations.
- **Local Incident Simulation & Sandbox Reproduction Framework**: Dev-environment incident reproduction pipeline using scenario fixtures (e.g. `testdata/scenarios/db_connection_exhaustion.json`) and Taskfile command `task incident:simulate` to verify the multi-agent clash and consensus without production outages, plus isolated container sandbox replay.

---

## Task Progress

### Phase 1: Database Schema & Migration Foundation
- [x] Task 1: Initialize Go project module and embedded migration runner (`internal/database`)
- [x] Task 2: Create DDL migrations for extensions and core tables (workspaces, users, personas)
- [x] Task 3: Create DDL migrations for document ingestion and pgvector chunks with `status` and `chunk_count`
- [x] Task 4: Create DDL migrations for incidents and debate turns with `severity` and composite indices

### Checkpoint: Foundation Schema
- [x] Embedded migrations verified via `internal/database/migrator_test.go`
- [x] Schema rollback / down migrations defined for all 4 migration pairs
- [x] HNSW vector indices and GIN full-text search indices configured

### Phase 2: Domain Entities & Value Objects (SOLID & Clean Code)
- [x] Task 5: Implement domain value objects (`WorkspaceID`, `PersonaKey`, `Severity`, `DocumentStatus`, `IncidentStatus`, `Stage`, `TurnType`, `SlackTypes`, `EmbeddingVector`, `CitationSnapshot`)
- [x] Task 6: Implement pure domain entities (`Workspace`, `User`, `Persona`, `Document`, `DocumentChunk`, `Incident`, `DebateTurn`)
- [x] Task 7: Define repository interface contracts with workspace-scoped signatures (`internal/domain/repository`)

### Checkpoint: Domain Models
- [x] Value objects and entity invariants are unit tested (`go test -v ./internal/domain/...`); measured coverage is 72.3% (value) and 50.3% (entity), not the 100% this line previously claimed
- [x] Domain layer has zero external framework or database driver dependencies (pure Go standard library + UUID)

### Phase 3: Repository Implementation & Multi-Tenant Integration Tests
- [x] Task 8: Implement PostgreSQL repository for workspaces and personas (`internal/infrastructure/postgres`)
- [x] Task 9: Implement PostgreSQL repository for documents and pgvector chunks (including RRF `SearchHybrid`)
- [x] Task 10: Implement PostgreSQL repository for incidents and debate turns
- [x] Task 11: Implement `TransactionManager` and integration tests verifying cross-tenant isolation

### Checkpoint: Complete Foundation
- [x] All unit and repository integration tests pass
- [x] Cross-tenant data leakage tests explicitly verify isolation
- [x] Vector similarity search query verifies persona and workspace scoping

### Phase 4: Local Incident Simulation & Sandbox Reproduction Framework
- [x] Task 12: Define scenario fixtures for common failure modes (`db_connection_exhaustion.json`, `checkout_packet_loss.json`, `oom_crashloop.json`)
- [ ] Task 13: Implement `task incident:simulate` command stepping through Stage 1, Stage 2, and Stage 3 with diagnostic assertions — **not built**. No `cmd/simulate`, no Taskfile entry; the Phase 4 fixtures are consumed only by `scripts/sandbox_reproduce.sh` / `task sandbox:reproduce`, which replays a scenario in a throwaway container rather than driving the orchestrator directly.
- [x] Task 14: Implement isolated dev/sandbox reproduction pipeline for replaying incident parameters in local test containers

### Phase 5: Observability Ingestion & Diagnostic Tool Adapters (MCP)
- [x] Task 15: Implement `IncidentContext` entity and `AlertHandler` for Grafana and AWS CloudWatch webhooks
- [ ] Task 16: Define `DiagnosticTool` / MCP interface contract for scoped read-only telemetry probing in Stage 1 — **built, then deleted**. A `telemetry.Registry` + `PrometheusTool` existed but were wired with zero registered tools and never read by the orchestrator (`o.tools` was a write-only field); removed as dead code rather than left half-wired. Re-add when a real probe is actually needed.

### Phase 6: Runtime Assembly (enablers the earlier phases assumed)
- [x] Task 17: `JobQueue` interface and in-process worker pool with backpressure (`internal/job`)
- [x] Task 18: Server dependency container and graceful shutdown (`internal/server`)
- [x] Task 19: LLM layer: Claude chat provider, OpenAI-compatible embeddings, offline fakes (`internal/infrastructure/llm`)
- [x] Task 20: 2-Stage Clash orchestrator with structured outputs and citation filtering (`internal/service/orchestrator`)
- [x] Task 21: Chunking and asynchronous ingestion service (`internal/service/ingest`)
- [x] Task 22: Router, middleware chain and composition root (`cmd/criaisis`)

### Checkpoint: Runnable Binary
- [x] `go build ./...`, `go vet ./...` and `go test -race ./...` all pass
- [x] Migration 000005 adds per-workspace webhook credentials (SHA-256 digest, constant-time verify)
- [x] End-to-end simulation verified against a live pgvector database — Docker was confirmed available; migrations apply cleanly (`go run ./cmd/criaisis -migrate-only`), and `CRIAISIS_TEST_INTEGRATION=1 go test -race ./...` passes against it, including the cross-tenant isolation suite and a workspace-provisioning rollback test. This surfaced and fixed a real bug: the pgvector binary type was never registered on the connection pool, so `chunk_repo`'s `BatchCreate` (used by document ingestion) silently corrupted every embedding it wrote.

### Phase 7: Security hardening, dead-code removal, and CI (this session)
- [x] Closed three reachable SSRF paths: tenant-controlled embeddings `base_url` (reachable with only a workspace admin key — validated as an https-only, non-private-IP destination before any request), the Slack/Discord webhook host check (was a bare `strings.HasSuffix`, so `evilslack.com` passed as a Slack host), and unrestricted HTTP redirects on the Slack/Discord/SNS outbound clients
- [x] Added the missing `workspace_id` predicate to `sandbox_reproduction_repo`'s three queries (the one repository without it) and extended the live-DB isolation suite to cover it
- [x] Made workspace provisioning (workspace + settings + 4 default personas) atomic via `PostgresTxManager`, previously built but never called from anywhere; proven with a live-DB rollback test
- [x] Deleted the Task 16 telemetry/diagnostic-tool subsystem (dead), the orphan `entity.User` aggregate (no repo, no handler, never referenced), and moved `orchestrator.NewStubChat` out of the production binary into a test file
- [x] Added `.golangci.yml` (gosec, unconvert, unparam plus golangci-lint's default set) and fixed every finding — including a real crash (`job.MemoryQueue.Enqueue` could panic on a channel the concurrent `Shutdown` had just closed) and a real RCE-class dependency finding (`next@16.3.5` carried a critical CVE range; bumped to `16.3.8`)
- [x] Stood up `.github/workflows/ci.yml`: build/vet/lint/govulncheck/unit tests with a coverage floor/integration tests against a pgvector service container (backend), a TruffleHog secret scan, and lint/typecheck/test/build (dashboard) — closing the "no CI exists" gap entirely
- [x] Fixed the broken `web/` `npm run lint` (Next.js 16 removed `next lint`; added a standalone `eslint.config.mjs`)
- [x] Added tests for the four packages that had none: `service/tenant` (0% → 83.7%), `infrastructure/slack` (0% → 91.5%), `service/incident` (0% → 76.7%), `server` (0% → 69.2%). Aggregate coverage: 42.5% → 47.7% — still well short of the org's 80% floor; CI's coverage gate is a 47% regression-guard ratchet, not a claim the mandate is met.

### Not started
- Slack app OAuth install, slash commands, and Block Kit rendering, and `@mention` routing, **are implemented** (`internal/handler/slack_inbound.go`, mounted in `internal/router/router.go`) — this line was stale.
- Web dashboard: runbook management, persona prompt editor, and transcript viewer **are implemented** (`web/src/app/{runbooks,specialists,incidents}`) — this line was stale.
- Genuinely remaining: the Slack OAuth callback has no `state` parameter (CSRF hardening — needs a new install-initiation endpoint to mint and later verify a nonce, not a one-line fix); SNS message `Signature`/`SigningCertURL` are never verified (lower priority — the alert-ingest endpoint is already gated by a per-workspace secret token before any SNS parsing happens); `task incident:simulate` (Task 13, above); raising aggregate test coverage toward the org's 80% floor.
