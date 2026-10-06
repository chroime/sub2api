package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const modelTemplatesSettingKey = "upstream_governance_model_templates"

func decodeModelTemplates(raw string) (*ModelTemplates, error) {
	var value ModelTemplates
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return normalizeModelTemplates(value)
}

func (s *sqlStore) LoadModelTemplates(ctx context.Context) (*ModelTemplates, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, modelTemplatesSettingKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return &ModelTemplates{Templates: []ModelTemplate{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeModelTemplates(raw)
}

func (s *sqlStore) SaveModelTemplates(ctx context.Context, value *ModelTemplates, expected int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The transaction lock also serializes the first write, when no settings row
	// exists yet. Row locking alone cannot protect that case.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, modelTemplatesSettingKey); err != nil {
		return err
	}
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1 FOR UPDATE`, modelTemplatesSettingKey).Scan(&raw)
	var current int64
	if err == nil {
		stored, parseErr := decodeModelTemplates(raw)
		if parseErr != nil {
			return parseErr
		}
		current = stored.Version
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current != expected {
		return ErrConflict
	}
	saved := *value
	saved.Version = current + 1
	encoded, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings (key,value,updated_at) VALUES ($1,$2,NOW()) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at`, modelTemplatesSettingKey, string(encoded)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	value.Version = saved.Version
	return nil
}
