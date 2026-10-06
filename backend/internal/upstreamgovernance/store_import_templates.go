package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
)

const importTemplatesSettingKey = "upstream_governance_import_templates"

var _ importTemplateStore = (*sqlStore)(nil)

func decodeImportTemplates(raw string) (*ImportTemplates, error) {
	var value ImportTemplates
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return normalizeImportTemplates(value)
}

func (s *sqlStore) LoadImportTemplates(ctx context.Context) (*ImportTemplates, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, importTemplatesSettingKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return &ImportTemplates{Templates: []ImportTemplate{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeImportTemplates(raw)
}

// The advisory transaction lock covers the absent-row first write; collection
// version comparison is performed only after that lock is held. This method
// writes one dedicated settings key and never touches import/account state.
func (s *sqlStore) SaveImportTemplates(ctx context.Context, value *ImportTemplates, expected int64) error {
	if value == nil || expected < 0 || expected == math.MaxInt64 || value.Version != expected {
		return ErrInvalid
	}
	saved, err := normalizeImportTemplates(*value)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, importTemplatesSettingKey); err != nil {
		return err
	}
	var raw string
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1 FOR UPDATE`, importTemplatesSettingKey).Scan(&raw)
	if err == nil {
		stored, parseErr := decodeImportTemplates(raw)
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
	saved.Version = current + 1
	encoded, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings (key,value,updated_at) VALUES ($1,$2,NOW()) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at`, importTemplatesSettingKey, string(encoded)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	*value = *saved
	return nil
}
