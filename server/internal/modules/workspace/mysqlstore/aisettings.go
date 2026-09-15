package mysqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/chenbb0128/weavepress/server/internal/modules/aisettings"
)

type AISettingsStore struct {
	db *sql.DB
}

func NewAISettingsStore(db *sql.DB) *AISettingsStore {
	return &AISettingsStore{db: db}
}

var _ aisettings.Store = (*AISettingsStore)(nil)

func (s *AISettingsStore) GetRuntime(ctx context.Context) (aisettings.StoredRuntime, error) {
	var value aisettings.StoredRuntime
	var updatedBy sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT enabled, active_provider, updated_by, created_at, updated_at FROM ai_runtime_settings WHERE id = 1`).Scan(
		&value.Enabled, &value.ActiveProvider, &updatedBy, &value.CreatedAt, &value.UpdatedAt,
	)
	if err != nil {
		return aisettings.StoredRuntime{}, err
	}
	if updatedBy.Valid {
		id := uint64(updatedBy.Int64)
		value.UpdatedBy = &id
	}
	return value, nil
}

func (s *AISettingsStore) ListProviders(ctx context.Context) ([]aisettings.StoredProvider, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider, base_url, model, api_key_ciphertext, updated_by, created_at, updated_at FROM ai_provider_settings ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]aisettings.StoredProvider, 0, 3)
	for rows.Next() {
		value, err := scanAIProvider(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *AISettingsStore) GetProvider(ctx context.Context, provider string) (aisettings.StoredProvider, error) {
	value, err := scanAIProvider(s.db.QueryRowContext(ctx, `SELECT provider, base_url, model, api_key_ciphertext, updated_by, created_at, updated_at FROM ai_provider_settings WHERE provider = ?`, provider))
	if errors.Is(err, sql.ErrNoRows) {
		return aisettings.StoredProvider{}, aisettings.ErrProviderNotConfigured
	}
	return value, err
}

func (s *AISettingsStore) Update(ctx context.Context, input aisettings.StoreUpdate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_provider_settings
		(provider, base_url, model, api_key_ciphertext, updated_by)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		base_url = VALUES(base_url), model = VALUES(model),
		api_key_ciphertext = IF(?, api_key_ciphertext, VALUES(api_key_ciphertext)),
		updated_by = VALUES(updated_by)`,
		input.Provider, input.BaseURL, input.Model, nullableBytes(input.APICiphertext), input.UpdatedBy, input.PreserveKey,
	)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_runtime_settings SET enabled = ?, active_provider = ?, updated_by = ? WHERE id = 1`, input.Enabled, input.ActiveProvider, input.UpdatedBy); err != nil {
		return err
	}
	return tx.Commit()
}

func scanAIProvider(row scanner) (aisettings.StoredProvider, error) {
	var value aisettings.StoredProvider
	var ciphertext []byte
	err := row.Scan(&value.Provider, &value.BaseURL, &value.Model, &ciphertext, &value.UpdatedBy, &value.CreatedAt, &value.UpdatedAt)
	value.APICiphertext = append([]byte(nil), ciphertext...)
	return value, err
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
