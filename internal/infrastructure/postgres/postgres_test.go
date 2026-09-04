package postgres

import (
	"context"
	"testing"

	"criaisis/internal/domain/repository"
)

// Verify interface implementations at compile time
var (
	_ repository.WorkspaceRepository  = (*PostgresWorkspaceRepository)(nil)
	_ repository.PersonaRepository    = (*PostgresPersonaRepository)(nil)
	_ repository.DocumentRepository   = (*PostgresDocumentRepository)(nil)
	_ repository.ChunkRepository      = (*PostgresChunkRepository)(nil)
	_ repository.IncidentRepository   = (*PostgresIncidentRepository)(nil)
	_ repository.DebateTurnRepository = (*PostgresDebateTurnRepository)(nil)
	_ repository.TransactionManager   = (*PostgresTxManager)(nil)
)

func TestTxContext_Propagation(t *testing.T) {
	ctx := context.Background()

	// Initial context has no transaction
	if tx := TxFromContext(ctx); tx != nil {
		t.Error("expected nil tx from clean background context")
	}

	// Without active tx, GetExecutor returns the provided pool
	exec := GetExecutor(ctx, nil)
	if exec != nil {
		t.Error("expected nil executor when pool is nil and no tx is in context")
	}
}
