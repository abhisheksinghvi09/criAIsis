package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"criaisis/internal/config"
	"criaisis/internal/database"
	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/postgres"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// getTestPool connects to a running PostgreSQL 16 + pgvector database if configured.
// If the database is not accessible or CRIAISIS_TEST_INTEGRATION is unset, the test is gracefully skipped.
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("CRIAISIS_TEST_INTEGRATION") == "" {
		t.Skip("skipping postgres integration test; set CRIAISIS_TEST_INTEGRATION=1 to run against live database")
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Skipf("cannot load config for integration test: %v", err)
	}

	logger := zerolog.Nop()
	db, err := database.New(cfg, &logger)
	if err != nil {
		t.Skipf("skipping integration test, cannot connect to database: %v", err)
	}

	return db.Pool
}

func TestTenantIsolation_RepositoryQueries(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := postgres.NewWorkspaceRepository(pool)
	docRepo := postgres.NewDocumentRepository(pool)
	chunkRepo := postgres.NewChunkRepository(pool)
	incRepo := postgres.NewIncidentRepository(pool)

	// 1. Create two distinct workspaces (Tenant Alpha and Tenant Beta)
	wsAlpha, err := entity.NewWorkspace("T_ALPHA_123", "Workspace Alpha", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("failed creating wsAlpha entity: %v", err)
	}
	wsBeta, err := entity.NewWorkspace("T_BETA_456", "Workspace Beta", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("failed creating wsBeta entity: %v", err)
	}

	wsAlphaID := wsAlpha.ID()
	wsBetaID := wsBeta.ID()

	if err := wsRepo.Create(ctx, wsAlpha); err != nil {
		t.Fatalf("failed persisting wsAlpha: %v", err)
	}
	if err := wsRepo.Create(ctx, wsBeta); err != nil {
		t.Fatalf("failed persisting wsBeta: %v", err)
	}

	// Clean up after test
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM workspaces WHERE id IN ($1, $2)", wsAlphaID.UUID(), wsBetaID.UUID())
	}()

	// 2. Create Incidents in both workspaces
	incAlpha, err := entity.NewIncident(
		wsAlphaID,
		"Alpha Incident",
		"Alpha desc",
		value.SlackChannelID("C_ALPHA"),
		value.SlackThreadTS("1700000001.000100"),
		value.SeveritySev1,
		value.SlackUserID("U_ALPHA"),
		json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("failed creating incAlpha: %v", err)
	}
	incBeta, err := entity.NewIncident(
		wsBetaID,
		"Beta Incident",
		"Beta desc",
		value.SlackChannelID("C_BETA"),
		value.SlackThreadTS("1700000002.000100"),
		value.SeveritySev2,
		value.SlackUserID("U_BETA"),
		json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("failed creating incBeta: %v", err)
	}

	if err := incRepo.Create(ctx, incAlpha); err != nil {
		t.Fatalf("failed saving incAlpha: %v", err)
	}
	if err := incRepo.Create(ctx, incBeta); err != nil {
		t.Fatalf("failed saving incBeta: %v", err)
	}

	// 3. Verify Cross-Tenant Isolation on Incidents
	// Querying by Beta's ID using Alpha's workspace ID MUST return not found
	_, err = incRepo.GetByID(ctx, wsAlphaID, incBeta.ID())
	if err == nil {
		t.Error("CRITICAL DATA LEAKAGE: Tenant Alpha retrieved Tenant Beta's incident by ID")
	}

	// Querying by Beta's thread timestamp using Alpha's workspace ID MUST return not found
	_, err = incRepo.GetBySlackThread(ctx, wsAlphaID, value.SlackChannelID("C_BETA"), value.SlackThreadTS("1700000002.000100"))
	if err == nil {
		t.Error("CRITICAL DATA LEAKAGE: Tenant Alpha retrieved Tenant Beta's incident by Slack Thread")
	}

	// ListActive on Alpha must contain incAlpha but NEVER incBeta
	activeAlpha, err := incRepo.ListActive(ctx, wsAlphaID, 50, 0)
	if err != nil {
		t.Fatalf("failed listing active incidents for Alpha: %v", err)
	}
	for _, inc := range activeAlpha {
		if inc.WorkspaceID() != wsAlphaID {
			t.Errorf("CRITICAL DATA LEAKAGE: ListActive for Alpha returned incident belonging to %s", inc.WorkspaceID())
		}
		if inc.ID() == incBeta.ID() {
			t.Error("CRITICAL DATA LEAKAGE: ListActive for Alpha contained Beta's incident")
		}
	}

	// 4. Create Documents in both workspaces
	pAlphaID := uuid.New()
	pBetaID := uuid.New()

	docAlpha, err := entity.NewDocument(wsAlphaID, pAlphaID, "Alpha Runbook", "# Database Runbook Alpha")
	if err != nil {
		t.Fatalf("failed creating docAlpha: %v", err)
	}
	docBeta, err := entity.NewDocument(wsBetaID, pBetaID, "Beta Runbook", "# Database Runbook Beta")
	if err != nil {
		t.Fatalf("failed creating docBeta: %v", err)
	}

	if err := docRepo.Create(ctx, docAlpha); err != nil {
		t.Fatalf("failed saving docAlpha: %v", err)
	}
	if err := docRepo.Create(ctx, docBeta); err != nil {
		t.Fatalf("failed saving docBeta: %v", err)
	}

	// Querying docBeta using wsAlphaID MUST fail
	_, err = docRepo.GetByID(ctx, wsAlphaID, docBeta.ID())
	if err == nil {
		t.Error("CRITICAL DATA LEAKAGE: Tenant Alpha retrieved Tenant Beta's document by ID")
	}

	// 5. Test Chunk Hybrid Search Isolation
	embeddingVec, err := value.NewEmbeddingVector(make([]float32, 1536))
	if err != nil {
		t.Fatalf("failed creating embedding vector: %v", err)
	}

	chunkAlpha, err := entity.NewDocumentChunk(
		wsAlphaID,
		pAlphaID,
		docAlpha.ID(),
		0,
		"PostgreSQL connection timeout deadlock on orders table",
		12,
		embeddingVec,
		json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("failed creating chunkAlpha: %v", err)
	}

	chunkBeta, err := entity.NewDocumentChunk(
		wsBetaID,
		pBetaID,
		docBeta.ID(),
		0,
		"PostgreSQL connection timeout deadlock on customers table",
		12,
		embeddingVec,
		json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("failed creating chunkBeta: %v", err)
	}

	if err := chunkRepo.BatchCreate(ctx, []*entity.DocumentChunk{chunkAlpha, chunkBeta}); err != nil {
		t.Fatalf("failed batch creating chunks: %v", err)
	}

	// SearchHybrid under Alpha must NEVER return Beta chunks
	results, err := chunkRepo.SearchHybrid(ctx, wsAlphaID, pAlphaID, "PostgreSQL deadlock", embeddingVec, 10)
	if err != nil {
		t.Fatalf("failed executing SearchHybrid: %v", err)
	}

	for _, res := range results {
		if res.Chunk.DocumentID() == docBeta.ID() {
			t.Error("CRITICAL DATA LEAKAGE: SearchHybrid in Alpha workspace returned Beta document chunk")
		}
		if res.Chunk.ID() == chunkBeta.ID() {
			t.Error("CRITICAL DATA LEAKAGE: SearchHybrid in Alpha workspace returned Beta chunk")
		}
	}
}
