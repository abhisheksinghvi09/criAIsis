package postgres

import (
	"context"
	"fmt"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"
	"criaisis/internal/domain/value"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// settingsColumns is the read projection, kept in one place so the scan order
// cannot drift away from the query.
const settingsColumns = `workspace_id,
	llm_provider, llm_api_key_encrypted, llm_specialist_model, llm_synthesis_model,
	embedding_provider, embedding_api_key_encrypted, embedding_model, embedding_base_url,
	notify_provider, notify_webhook_url_encrypted,
	created_at, updated_at`

// PostgresSettingsRepository stores per-tenant integration credentials.
type PostgresSettingsRepository struct {
	pool *pgxpool.Pool
}

var _ repository.SettingsRepository = (*PostgresSettingsRepository)(nil)

// NewSettingsRepository constructs the adapter.
func NewSettingsRepository(pool *pgxpool.Pool) *PostgresSettingsRepository {
	return &PostgresSettingsRepository{pool: pool}
}

// Create stores the initial settings row for a workspace.
func (r *PostgresSettingsRepository) Create(ctx context.Context, s *entity.WorkspaceSettings) error {
	const query = `
		INSERT INTO workspace_settings (` + settingsColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);
	`
	exec := GetExecutor(ctx, r.pool)
	_, err := exec.Exec(ctx, query,
		s.WorkspaceID().UUID(),
		s.LLMProvider(), s.LLMAPIKeyEncrypted(), s.SpecialistModel(), s.SynthesisModel(),
		s.EmbeddingProvider(), s.EmbeddingAPIKeyEncrypted(), s.EmbeddingModel(), s.EmbeddingBaseURL(),
		s.NotifyProvider(), s.NotifyWebhookEncrypted(),
		s.CreatedAt(), s.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("inserting workspace settings: %w", err)
	}
	return nil
}

// Get retrieves a tenant's settings, scoped to that tenant alone.
func (r *PostgresSettingsRepository) Get(ctx context.Context, wsID value.WorkspaceID) (*entity.WorkspaceSettings, error) {
	const query = `SELECT ` + settingsColumns + ` FROM workspace_settings WHERE workspace_id = $1;`

	exec := GetExecutor(ctx, r.pool)
	return scanSettings(exec.QueryRow(ctx, query, wsID.UUID()))
}

// Update persists changes, refusing silently-missing rows.
func (r *PostgresSettingsRepository) Update(ctx context.Context, s *entity.WorkspaceSettings) error {
	const query = `
		UPDATE workspace_settings SET
			llm_provider = $2, llm_api_key_encrypted = $3,
			llm_specialist_model = $4, llm_synthesis_model = $5,
			embedding_provider = $6, embedding_api_key_encrypted = $7,
			embedding_model = $8, embedding_base_url = $9,
			notify_provider = $10, notify_webhook_url_encrypted = $11,
			updated_at = $12
		WHERE workspace_id = $1;
	`
	exec := GetExecutor(ctx, r.pool)
	tag, err := exec.Exec(ctx, query,
		s.WorkspaceID().UUID(),
		s.LLMProvider(), s.LLMAPIKeyEncrypted(), s.SpecialistModel(), s.SynthesisModel(),
		s.EmbeddingProvider(), s.EmbeddingAPIKeyEncrypted(), s.EmbeddingModel(), s.EmbeddingBaseURL(),
		s.NotifyProvider(), s.NotifyWebhookEncrypted(),
		s.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("updating workspace settings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no settings row for workspace %s", s.WorkspaceID())
	}
	return nil
}

// scanSettings maps a row onto the domain entity.
func scanSettings(row pgx.Row) (*entity.WorkspaceSettings, error) {
	var (
		wsID                                         [16]byte
		llmProvider, specialistModel, synthesisModel string
		llmKey                                       []byte
		embeddingProvider, embeddingModel, baseURL   string
		embeddingKey                                 []byte
		notifyProvider                               string
		notifyWebhook                                []byte
		createdAt, updatedAt                         time.Time
	)

	if err := row.Scan(
		&wsID,
		&llmProvider, &llmKey, &specialistModel, &synthesisModel,
		&embeddingProvider, &embeddingKey, &embeddingModel, &baseURL,
		&notifyProvider, &notifyWebhook,
		&createdAt, &updatedAt,
	); err != nil {
		return nil, fmt.Errorf("scanning workspace settings: %w", err)
	}

	parsed, err := value.FromUUID(wsID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace id in settings row: %w", err)
	}

	return entity.ReconstituteWorkspaceSettings(
		parsed,
		llmProvider, llmKey, specialistModel, synthesisModel,
		embeddingProvider, embeddingKey, embeddingModel, baseURL,
		notifyProvider, notifyWebhook,
		createdAt, updatedAt,
	), nil
}
