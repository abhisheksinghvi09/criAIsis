# Implementation Plan: Database Schema & Domain Model Foundation

## Overview
criAIsis is a multi-tenant AI incident debate engine running inside Slack threads, powered by a 2-Stage Asynchronous Clash among 4 specialist AI personas (Network, Database, Application, Security) grounded in runbook documents. This plan details the foundation phase: establishing the PostgreSQL 16 + pgvector relational and vector data store, domain entities and value objects adhering to SOLID principles and Clean Architecture, database migration infrastructure, and repository layer with strict multi-tenant isolation.

## Architecture Decisions

- **PostgreSQL 16 with pgvector**: Relational entities (`workspaces`, `users`, `personas`, `incidents`, `debate_turns`) and vector embeddings (`vector(1536)`) reside in a single database. This eliminates dual-write drift and removes the operational overhead of a standalone vector database.
- **Multi-Tenant Scoping at Schema & Repo Layers**: Every table downstream of `workspaces` contains `workspace_id UUID NOT NULL` with foreign key cascades. Every repository method takes `workspace_id` and enforces it in the SQL `WHERE` clause.
- **HNSW Cosine Vector Index**: `document_chunks` index uses HNSW with `vector_cosine_ops`, `m = 16`, and `ef_construction = 64` for sub-50ms nearest neighbor search.
- **Clean Architecture & Domain Value Objects**: Domain models use strongly typed value objects (`WorkspaceID`, `PersonaKey`, `IncidentStatus`, `Stage`, `TurnType`, `EmbeddingVector`) rather than raw primitives, enforcing invariants on creation.
- **Go Migration Engine**: Use SQL migration files executed via a robust migration tool (`golang-migrate` / standard migration runner) to ensure deterministic schema versioning.

## Task List

### Phase 1: Database Schema & Migration Foundation
- [ ] Task 1: Initialize Go project module and migration runner
- [ ] Task 2: Create DDL migrations for extensions and core tables (workspaces, users, personas)
- [ ] Task 3: Create DDL migrations for document ingestion and pgvector chunks
- [ ] Task 4: Create DDL migrations for incidents and debate turns

### Checkpoint: Foundation Schema
- [ ] Migrations apply cleanly to a clean PostgreSQL 16 + pgvector instance
- [ ] Schema rollback / down migrations execute without errors
- [ ] Table constraints, cascades, and HNSW vector indices are verified

### Phase 2: Domain Entities & Value Objects (SOLID & Clean Code)
- [ ] Task 5: Implement domain value objects with validation invariants
- [ ] Task 6: Implement domain entities (Workspace, User, Persona, Document, DocumentChunk, Incident, DebateTurn)
- [ ] Task 7: Define repository interface contracts with workspace-scoped signatures

### Checkpoint: Domain Models
- [ ] Unit tests for all value objects and entity invariants pass with 100% coverage
- [ ] Domain layer has zero external framework or database driver dependencies (pure Go)

### Phase 3: Repository Implementation & Multi-Tenant Integration Tests
- [ ] Task 8: Implement PostgreSQL repository for workspaces and personas
- [ ] Task 9: Implement PostgreSQL repository for documents and pgvector chunks
- [ ] Task 10: Implement PostgreSQL repository for incidents and debate turns
- [ ] Task 11: Implement integration tests verifying cross-tenant isolation

### Checkpoint: Complete Foundation
- [ ] All unit and repository integration tests pass
- [ ] Cross-tenant data leakage tests explicitly verify isolation
- [ ] Vector similarity search query verifies persona and workspace scoping

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Cross-tenant data leakage | Critical | Enforce `workspace_id` at the database foreign key level, composite index level, and repository method parameter level. Add dedicated cross-tenant test suites. |
| pgvector performance degradation under load | Medium | Use HNSW index with tuned `m = 16` and `ef_construction = 64` and filter by compound `(workspace_id, persona_id)` before distance ranking. |
| Slack bot token leakage | Critical | Store tokens encrypted using AES-GCM-256 (`slack_bot_token_encrypted BYTEA`). Never expose raw tokens to logs or frontend responses. |
| Persona prompt drift | Low | Seed the 4 fixed personas (`network`, `database`, `application`, `security`) automatically upon workspace creation via a deterministic seeder. |

## Open Questions

- Should document chunking in Stage 1 RAG support markdown header-based boundaries in addition to fixed-size token splitting? (Recommended: Start with ~500 token recursive splitting with 10% overlap, with header-aware splitting as an enhancement).
- Should default persona system prompts be loaded from embedded YAML/text files or hardcoded Go constants? (Recommended: Embedded default template files in `internal/domain/templates`).

