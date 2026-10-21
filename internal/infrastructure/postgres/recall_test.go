package postgres_test

import (
	"context"
	"math/rand/v2"
	"testing"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
	"criaisis/internal/infrastructure/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

// vec builds a unit-ish vector pointing mostly along axis, with seeded noise.
func vec(t *testing.T, axis int, noise float32, r *rand.Rand) value.EmbeddingVector {
	t.Helper()
	v := make([]float32, value.ExpectedEmbeddingDimensions)
	v[axis] = 1
	for i := range v {
		v[i] += noise * (r.Float32() - 0.5)
	}
	ev, err := value.NewEmbeddingVector(v)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// seedTenant creates a workspace with one persona and document holding n chunks.
func seedTenant(t *testing.T, pool *pgxpool.Pool, team string, n, axis int, r *rand.Rand) (value.WorkspaceID, *entity.Persona) {
	t.Helper()
	ctx := context.Background()
	ws, _ := entity.NewWorkspace(team, team, []byte("x"))
	if err := postgres.NewWorkspaceRepository(pool).Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	persona, _ := entity.NewPersona(ws.ID(), value.PersonaKeyDatabase, "DB", "prompt")
	if err := postgres.NewPersonaRepository(pool).Create(ctx, persona); err != nil {
		t.Fatal(err)
	}
	doc, _ := entity.NewDocument(ws.ID(), persona.ID(), team+" runbook", "body")
	if err := postgres.NewDocumentRepository(pool).Create(ctx, doc); err != nil {
		t.Fatal(err)
	}
	chunks := make([]*entity.DocumentChunk, 0, n)
	for i := 0; i < n; i++ {
		c, err := entity.NewDocumentChunk(ws.ID(), persona.ID(), doc.ID(), i, "alpha beta gamma", 3, vec(t, axis, 0.05, r), nil)
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
	}
	if err := postgres.NewChunkRepository(pool).BatchCreate(ctx, chunks); err != nil {
		t.Fatal(err)
	}
	return ws.ID(), persona
}

// A small tenant next to a large one must still get dense results. HNSW returns
// the global nearest candidates and filters afterwards, so without iterative
// scans a big neighbour whose vectors sit closer to the query crowds the small
// tenant out entirely (measured: 0 of 20 hits; with iterative scans, 19).
func TestSearchHybrid_SmallTenantKeepsDenseRecallBesideALargeOne(t *testing.T) {
	pool := isolatedPool(t)
	r := rand.New(rand.NewPCG(1, 2))

	seedTenant(t, pool, "T_BIG", 3000, 0, r)                          // crowded around the query
	smallWS, smallPersona := seedTenant(t, pool, "T_SMALL", 60, 1, r) // farther away

	// Force the plan this guards against. Normally the planner prefers the scope
	// B-tree and an exact sort; it picks HNSW once a tenant is a large share of the
	// table or statistics drift, and HNSW filters only after taking its nearest
	// candidates, which here all belong to the big tenant.
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DROP INDEX idx_chunks_scope"); err != nil {
		t.Fatal(err)
	}
	var db string
	_ = pool.QueryRow(ctx, "SELECT current_database()").Scan(&db)
	if _, err := pool.Exec(ctx, "ALTER DATABASE "+db+" SET enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	pool.Reset() // new connections pick up the setting

	query := vec(t, 0, 0, r)
	// Text that matches nothing, so only the dense path can return results.
	results, err := postgres.NewChunkRepository(pool).SearchHybrid(context.Background(),
		smallWS, smallPersona.ID(), "zzzqqq", query, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("small tenant got %d dense results, want 5", len(results))
	}
	for _, res := range results {
		if res.Chunk.WorkspaceID() != smallWS {
			t.Fatal("search returned another tenant's chunk")
		}
	}
}
