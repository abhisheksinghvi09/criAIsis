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
- [x] 100% test coverage across all value objects and entity invariants (`go test -v ./internal/domain/...`)
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
- [ ] Task 12: Define scenario fixtures for common failure modes (`db_connection_exhaustion.json`, `checkout_packet_loss.json`)
- [ ] Task 13: Implement `task incident:simulate` command stepping through Stage 1, Stage 2, and Stage 3 with diagnostic assertions
- [ ] Task 14: Implement isolated dev/sandbox reproduction pipeline for replaying incident parameters in local test containers

### Phase 5: Observability Ingestion & Diagnostic Tool Adapters (MCP)
- [ ] Task 15: Implement `IncidentContext` entity and `AlertHandler` for Grafana and AWS CloudWatch webhooks
- [ ] Task 16: Define `DiagnosticTool` / MCP interface contract for scoped read-only telemetry probing in Stage 1
