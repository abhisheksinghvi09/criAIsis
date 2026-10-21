package postgres

import (
	"context"
	"fmt"

	"criaisis/internal/infrastructure/crypto"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// settingSecrets maps each encrypted settings column to its binding field.
var settingSecrets = []struct{ column, field string }{
	{"llm_api_key_encrypted", crypto.FieldLLMKey},
	{"embedding_api_key_encrypted", crypto.FieldEmbeddingKey},
	{"notify_webhook_url_encrypted", crypto.FieldNotifyWebhook},
	{"oncall_api_key_encrypted", crypto.FieldOnCallKey},
}

// RewrapCredentials re-seals every stored secret that is legacy (pre-envelope,
// unbound) or under a non-current key, using the current key and its binding.
// It runs in one transaction with the rows locked, so it is all-or-nothing and
// safe against a concurrent write; rerunning it is a no-op. It returns how many
// values it rewrote.
func RewrapCredentials(ctx context.Context, pool *pgxpool.Pool, c *crypto.Cipher) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	n := 0
	rewrap := func(table, column, idCol string, id any, sealed []byte, aad string) error {
		if !c.NeedsRewrap(sealed) {
			return nil
		}
		plain, err := c.Open(sealed, aad)
		if err != nil {
			return fmt.Errorf("%s.%s for %v cannot be opened with any configured key: %w", table, column, id, err)
		}
		resealed, err := c.Seal(plain, aad)
		if err != nil {
			return err
		}
		// table, column and idCol come from the constant lists in this file only.
		if _, err := tx.Exec(ctx, "UPDATE "+table+" SET "+column+" = $1 WHERE "+idCol+" = $2", resealed, id); err != nil {
			return err
		}
		n++
		return nil
	}

	// Slack bot tokens are bound to the Slack team id.
	rows, err := tx.Query(ctx, `SELECT id, slack_team_id, slack_bot_token_encrypted FROM workspaces FOR UPDATE`)
	if err != nil {
		return 0, err
	}
	type wsRow struct {
		id     uuid.UUID
		team   string
		sealed []byte
	}
	var wss []wsRow
	for rows.Next() {
		var r wsRow
		if err := rows.Scan(&r.id, &r.team, &r.sealed); err != nil {
			rows.Close()
			return 0, err
		}
		wss = append(wss, r)
	}
	rows.Close()
	for _, r := range wss {
		if err := rewrap("workspaces", "slack_bot_token_encrypted", "id", r.id, r.sealed, crypto.BotTokenAAD(r.team)); err != nil {
			return 0, err
		}
	}

	// Tenant credentials are bound to their workspace and field.
	for _, sec := range settingSecrets {
		rows, err := tx.Query(ctx, "SELECT workspace_id, "+sec.column+" FROM workspace_settings WHERE "+sec.column+" IS NOT NULL FOR UPDATE")
		if err != nil {
			return 0, err
		}
		type sRow struct {
			ws     uuid.UUID
			sealed []byte
		}
		var srs []sRow
		for rows.Next() {
			var r sRow
			if err := rows.Scan(&r.ws, &r.sealed); err != nil {
				rows.Close()
				return 0, err
			}
			srs = append(srs, r)
		}
		rows.Close()
		for _, r := range srs {
			if err := rewrap("workspace_settings", sec.column, "workspace_id", r.ws, r.sealed, crypto.SettingAAD(r.ws.String(), sec.field)); err != nil {
				return 0, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing rewrap: %w", err)
	}
	return n, nil
}
