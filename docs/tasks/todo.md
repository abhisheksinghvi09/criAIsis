# Task List: Database Schema, Models & Foundation

## Task 1: Initialize Go Project Module and Migration Runner

**Description:** Initialize Go module `criaisis`, configure dependency management (`go.mod`), setup Docker compose for local PostgreSQL 16 + pgvector, and configure the database migration runner.

**Acceptance criteria:**
- [ ] `go.mod` is initialized with Go 1.22+ and core dependencies (`pgx/v5`, `google/uuid`, `pgvector-go`).
- [ ] `docker-compose.yml` provides a PostgreSQL 16 service with `pgvector/pgvector:pg16`.
- [ ] Migration runner CLI/script can execute up/down SQL migrations deterministically.

**Verification:**
- [ ] Tests pass: `go test ./...`
- [ ] Build succeeds: `go build ./...`
- [ ] Manual check: Docker PostgreSQL starts with `vector` extension support enabled.

**Dependencies:** None

**Files likely touched:**
- `go.mod`
- `docker-compose.yml`
- `scripts/migrate.sh`

**Estimated scope:** Small (2-3 files)

---

## Task 2: DDL Migrations for Core Tables (Workspaces, Users, Personas)

**Description:** Create migration files defining `uuid-ossp`, `pgcrypto`, `vector` extensions and tables for `workspaces`, `users`, and `personas` with composite unique constraints and cascading foreign keys.

**Acceptance criteria:**
- [ ] `000001_init_core.up.sql` creates extensions and `workspaces`, `users`, `personas` tables.
- [ ] `slack_bot_token_encrypted` column is typed as `BYTEA`.
- [ ] Unique constraint `uq_workspace_persona_key (workspace_id, key)` is enforced.
- [ ] `000001_init_core.down.sql` cleanly drops tables and extensions in reverse order.

**Verification:**
- [ ] Tests pass: Migration up and down scripts run without SQL errors.
- [ ] Schema verify: `psql -c "\d personas"` confirms unique constraint and foreign keys.

**Dependencies:** Task 1

**Files likely touched:**
- `migrations/000001_init_core.up.sql`
- `migrations/000001_init_core.down.sql`

**Estimated scope:** Small (2 files)

---

## Task 3: DDL Migrations for Documents and pgvector Chunks

**Description:** Create migration files for `documents` and `document_chunks`, configuring HNSW cosine similarity vector index on `document_chunks.embedding vector(1536)` and compound scoping indices.

**Acceptance criteria:**
- [ ] `000002_documents_and_chunks.up.sql` creates `documents` and `document_chunks` tables with `workspace_id` foreign keys.
- [ ] HNSW index `idx_chunks_embedding_hnsw` is created using `vector_cosine_ops` with `m = 16, ef_construction = 64`.
- [ ] Compound index on `(workspace_id, persona_id, document_id)` exists for fast pre-filtering.
- [ ] `000002_documents_and_chunks.down.sql` drops tables cleanly.

**Verification:**
- [ ] Tests pass: Migration up and down execute cleanly.
- [ ] Schema verify: `psql -c "\d document_chunks"` displays HNSW index and vector(1536) column.

**Dependencies:** Task 2

**Files likely touched:**
- `migrations/000002_documents_and_chunks.up.sql`
- `migrations/000002_documents_and_chunks.down.sql`

**Estimated scope:** Small (2 files)

---

## Task 4: DDL Migrations for Incidents and Debate Turns

**Description:** Create migration files for `incidents` and `debate_turns`, capturing incident lifecycle state, Slack channel/thread identifiers, clash stage numbers, and referenced chunk ID arrays.

**Acceptance criteria:**
- [ ] `000003_incidents_and_debate_turns.up.sql` creates `incidents` and `debate_turns` tables.
- [ ] Unique constraint `uq_workspace_channel_thread (workspace_id, slack_channel_id, slack_thread_ts)` prevents duplicate debates in the same thread.
- [ ] `debate_turns.referenced_chunk_ids` is typed as `UUID[]` with index support.
- [ ] `000003_incidents_and_debate_turns.down.sql` drops tables cleanly.

**Verification:**
- [ ] Tests pass: Full migration suite applies from clean database and tears down cleanly.

**Dependencies:** Task 3

**Files likely touched:**
- `migrations/000003_incidents_and_debate_turns.up.sql`
- `migrations/000003_incidents_and_debate_turns.down.sql`

**Estimated scope:** Small (2 files)

---

## Checkpoint: Foundation Schema
- [ ] All migrations apply cleanly against PostgreSQL 16 + pgvector
- [ ] Down migrations rollback all changes cleanly
- [ ] Table constraints, indexes, and cascades verified

---

## Task 5: Domain Value Objects with Invariant Validation

**Description:** Implement domain Value Objects in `internal/domain/value` wrapping all domain primitives (`WorkspaceID`, `PersonaKey`, `IncidentStatus`, `Stage`, `TurnType`, `EmbeddingVector`, `EncryptedToken`) with pure constructors enforcing validation rules.

**Acceptance criteria:**
- [ ] `PersonaKey` only permits `network`, `database`, `application`, `security`.
- [ ] `Stage` only accepts 1, 2, or 3.
- [ ] `IncidentStatus` only accepts `investigating` and `resolved`.
- [ ] `EmbeddingVector` validates exact length of 1536 elements.
- [ ] Comprehensive unit tests verify valid and invalid parsing/construction.

**Verification:**
- [ ] Tests pass: `go test -v ./internal/domain/value/...`
- [ ] Build succeeds: `go build ./...`

**Dependencies:** Task 1

**Files likely touched:**
- `internal/domain/value/workspace_id.go`
- `internal/domain/value/persona_key.go`
- `internal/domain/value/incident_status.go`
- `internal/domain/value/stage.go`
- `internal/domain/value/embedding.go`
- `internal/domain/value/value_test.go`

**Estimated scope:** Medium (4-5 files)

---

## Task 6: Domain Entities and Invariant Enforcement

**Description:** Implement domain entities (`Workspace`, `User`, `Persona`, `Document`, `DocumentChunk`, `Incident`, `DebateTurn`) in `internal/domain/entity`, encapsulating fields and exposing behavior methods while protecting invariants.

**Acceptance criteria:**
- [ ] Entities hold unexported fields with getter methods and invariant-preserving state transitions (e.g., `Incident.Resolve()`, `Persona.UpdatePrompt()`).
- [ ] Value objects are used everywhere; zero raw primitive strings for IDs or enum states.
- [ ] Zero external dependencies outside standard library and `uuid`.

**Verification:**
- [ ] Tests pass: `go test -v ./internal/domain/entity/...`
- [ ] Code check: Methods under 10 lines, entities under 50 lines where feasible.

**Dependencies:** Task 5

**Files likely touched:**
- `internal/domain/entity/workspace.go`
- `internal/domain/entity/persona.go`
- `internal/domain/entity/document.go`
- `internal/domain/entity/incident.go`
- `internal/domain/entity/debate_turn.go`
- `internal/domain/entity/entity_test.go`

**Estimated scope:** Medium (5 files)

---

## Task 7: Repository Interface Contracts

**Description:** Define repository interfaces in `internal/domain/repository` adhering to Interface Segregation and Dependency Inversion. Every method requires explicit `context.Context` and `workspace_id` parameters.

**Acceptance criteria:**
- [ ] `WorkspaceRepository`, `PersonaRepository`, `DocumentRepository`, `ChunkRepository`, `IncidentRepository`, `DebateTurnRepository` interfaces are defined.
- [ ] Every multi-tenant query method enforces `wsID value.WorkspaceID`.
- [ ] `ChunkRepository.SearchSimilar` accepts `workspace_id`, `persona_id`, `queryEmbedding`, and `topK`.

**Verification:**
- [ ] Tests pass: `go test ./internal/domain/...`
- [ ] Build succeeds: `go build ./...`

**Dependencies:** Task 6

**Files likely touched:**
- `internal/domain/repository/workspace_repo.go`
- `internal/domain/repository/persona_repo.go`
- `internal/domain/repository/document_repo.go`
- `internal/domain/repository/incident_repo.go`

**Estimated scope:** Medium (4 files)

---

## Checkpoint: Domain Models
- [ ] Unit tests for all value objects and entity invariants pass with 100% coverage
- [ ] Zero database or network imports in `internal/domain/...`

---

## Task 8: PostgreSQL Repository for Workspaces and Personas

**Description:** Implement `WorkspaceRepository` and `PersonaRepository` using `pgx/v5`, mapping between domain entities and SQL rows, and including automatic seeding of the 4 default personas for new workspaces.

**Acceptance criteria:**
- [x] Workspace creation and retrieval work with encrypted bot tokens.
- [x] Persona retrieval by key and list by workspace enforce `workspace_id`.
- [x] Default 4 personas (`network`, `database`, `application`, `security`) are seeded on workspace initialization.

**Verification:**
- [x] Tests pass: `go test -v ./internal/infrastructure/postgres/...`

**Dependencies:** Tasks 4, 7

**Files likely touched:**
- `internal/infrastructure/postgres/workspace_repo.go`
- `internal/infrastructure/postgres/persona_repo.go`
- `internal/infrastructure/postgres/repo_test.go`

**Estimated scope:** Medium (3 files)

---

## Task 9: PostgreSQL Repository for Documents and pgvector Chunks

**Description:** Implement `DocumentRepository` and `ChunkRepository` with batch insertion and parameterized cosine similarity search scoped by `workspace_id` and `persona_id`.

**Acceptance criteria:**
- [x] `CreateBatch` efficiently inserts document chunks in a single transaction.
- [x] `SearchSimilar` executes `1 - (embedding <=> $1) AS similarity_score` filtered by `workspace_id = $2 AND persona_id = $3`.
- [x] Returns top-K results sorted by cosine distance ascending.

**Verification:**
- [x] Tests pass: Vector insertion and similarity query integration test returns expected nearest neighbors.

**Dependencies:** Tasks 4, 7

**Files likely touched:**
- `internal/infrastructure/postgres/document_repo.go`
- `internal/infrastructure/postgres/chunk_repo.go`
- `internal/infrastructure/postgres/chunk_repo_test.go`

**Estimated scope:** Medium (3 files)

---

## Task 10: PostgreSQL Repository for Incidents and Debate Turns

**Description:** Implement `IncidentRepository` and `DebateTurnRepository`, supporting incident creation, status transition (`investigating` -> `resolved`), thread lookup, and turn history insertion with referenced chunk IDs.

**Acceptance criteria:**
- [x] `GetBySlackThread` returns active incident for channel and thread timestamp.
- [x] `Resolve` updates status to `resolved` and sets `resolved_at = NOW()`.
- [x] `DebateTurnRepository.ListByIncident` returns turns in chronological sequence.

**Verification:**
- [x] Tests pass: Incident and debate turn repository integration tests pass.

**Dependencies:** Tasks 4, 7

**Files likely touched:**
- `internal/infrastructure/postgres/incident_repo.go`
- `internal/infrastructure/postgres/debate_turn_repo.go`
- `internal/infrastructure/postgres/incident_repo_test.go`

**Estimated scope:** Medium (3 files)

---

## Task 11: Multi-Tenant Isolation Integration Test Suite

**Description:** Create automated integration tests that simulate two distinct workspaces (`ws-alpha` and `ws-beta`), ingest documents into both, and verify that vector search, document retrieval, and incident listings never leak across tenant boundaries.

**Acceptance criteria:**
- [x] Vector search with `ws-alpha` token embedding never returns chunks belonging to `ws-beta`.
- [x] Querying incidents with `ws-alpha` never returns `ws-beta` incidents.
- [x] Cross-persona search within the same workspace never returns chunks from another persona.

**Verification:**
- [x] Tests pass: `go test -v -run TestTenantIsolation ./internal/infrastructure/postgres/...`

**Dependencies:** Tasks 8, 9, 10

**Files likely touched:**
- `internal/infrastructure/postgres/isolation_test.go`

**Estimated scope:** Small (1 file)

---

## Checkpoint: Complete Foundation
- [x] All migrations apply and rollback cleanly
- [x] 100% of domain value objects and entities covered by tests
- [x] All PostgreSQL repositories implemented and pass integration tests
- [x] Multi-tenant isolation verified with zero cross-tenant leakage

---

## Task 12: Scenario Fixtures for Incident Simulation

**Description:** Create deterministic JSON test fixtures representing common failure modes (`db_connection_exhaustion.json`, `checkout_packet_loss.json`, `oom_crashloop.json`), including runbook markdown, simulated error logs, stack traces, and ground-truth root cause classifications.

**Acceptance criteria:**
- [ ] Scenario fixtures include runbook excerpts for all 4 personas.
- [ ] Fixtures provide expected consensus classification (`Code-Level`, `Infrastructure`, `Hybrid`).

**Verification:**
- [ ] JSON fixtures validate against schema.

**Dependencies:** Task 11

**Files likely touched:**
- `testdata/scenarios/db_connection_exhaustion.json`
- `testdata/scenarios/checkout_packet_loss.json`

**Estimated scope:** Small (2-3 files)

---

## Task 13: Local Incident Simulation Runner

**Description:** Implement `task incident:simulate` CLI runner that seeds scenario runbooks, executes the 2-Stage Clash against simulated alerts, and asserts diagnostic classifications.

**Acceptance criteria:**
- [ ] Command runs end-to-end simulation in dev environment.
- [ ] Validates Stage 1 specialist citations and Stage 2 consensus classification against fixture expectations.

**Verification:**
- [ ] Run `task incident:simulate -- scenario=db_connection_exhaustion` succeeds.

**Dependencies:** Task 12

**Files likely touched:**
- `cmd/simulate/main.go`
- `Taskfile.yml`

**Estimated scope:** Medium (2 files)

---

## Task 14: Dev / Sandbox Incident Reproduction Pipeline

**Description:** Implement sandbox reproduction pipeline allowing engineers to recreate incident failure states (synthetic load, injected latency, simulated DB connection saturation) in isolated dev containers to test hypotheses safely.

**Acceptance criteria:**
- [ ] Sandbox script boots isolated Docker container reproducing incident conditions.
- [ ] Specialist agents can verify resolution in sandbox without touching production.

**Verification:**
- [ ] Automated container test passes locally.

**Dependencies:** Task 13

**Files likely touched:**
- `scripts/sandbox_reproduce.sh`
- `deployments/docker-compose.sandbox.yml`

**Estimated scope:** Medium (2-3 files)

---

## Task 15: Observability Alert Webhook Ingestion Handler

**Description:** Implement `AlertHandler` and router for `POST /api/v1/integrations/alerts/:provider` supporting Grafana Alerting and AWS CloudWatch SNS webhooks, extracting logs, stack traces, and metrics into `IncidentContext`.

**Acceptance criteria:**
- [ ] Webhook signature verification succeeds for supported providers.
- [ ] Ingests alert payload and creates incident with `trigger_type = 'webhook'`.
- [ ] Enqueues clash job to worker queue.

**Verification:**
- [ ] Unit & integration tests pass with sample Grafana and CloudWatch payloads.

**Dependencies:** Task 10

**Files likely touched:**
- `internal/handler/alert_handler.go`
- `internal/handler/alert_handler_test.go`
- `internal/domain/entity/incident_context.go`

**Estimated scope:** Medium (3 files)

---

## Task 16: Read-Only Diagnostic Tool Adapter Contract (MCP)

**Description:** Define Go interface contract for Model Context Protocol (MCP) and read-only telemetry adapters (Prometheus, CloudWatch Logs, K8s read-only), enforcing 5s timeouts and strict zero-mutation guarantees.

**Acceptance criteria:**
- [ ] Interface defines `ExecuteDiagnosticQuery(ctx, toolName, params) (QueryResult, error)`.
- [ ] Zero write/mutation methods permitted in interface.
- [ ] Implements graceful degradation when telemetry endpoint is slow or unreachable.

**Verification:**
- [ ] Unit tests verify mock adapter query execution and timeout cancellation.

**Dependencies:** Task 15

**Files likely touched:**
- `internal/domain/repository/diagnostic_tool.go`
- `internal/infrastructure/telemetry/mcp_adapter.go`
- `internal/infrastructure/telemetry/mcp_adapter_test.go`

**Estimated scope:** Medium (3 files)


