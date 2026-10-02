package upstreamgovernance

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestImportTemplatesSQLInitialReadIsEmptyAndReadOnly(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery("SELECT value FROM settings WHERE key=\\$1").WithArgs("upstream_governance_import_templates").WillReturnError(sql.ErrNoRows)
	value, err := s.(*sqlStore).LoadImportTemplates(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(0), value.Version)
	require.NotNil(t, value.Templates)
	require.Empty(t, value.Templates)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestImportTemplatesSQLSaveUsesIndependentKeyAndVersionLock(t *testing.T) {
	s, m := storeFixture(t)
	input := importTemplateFixture()
	saved := importTemplateFixture()
	saved.Version = 1
	raw, err := json.Marshal(saved)
	require.NoError(t, err)
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("upstream_governance_import_templates").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT value FROM settings WHERE key=\\$1 FOR UPDATE").WithArgs("upstream_governance_import_templates").WillReturnError(sql.ErrNoRows)
	m.ExpectExec("INSERT INTO settings").WithArgs("upstream_governance_import_templates", string(raw)).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	result, err := NewService(s, nil, nil, nil, false).SaveImportTemplates(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, saved, resultValue(result))
	require.Equal(t, int64(0), input.Version)
	require.NoError(t, m.ExpectationsWereMet())
}

func resultValue(value *ImportTemplates) ImportTemplates {
	if value == nil {
		return ImportTemplates{}
	}
	return *value
}

func TestImportTemplatesSQLConflictRollsBackWithoutWriting(t *testing.T) {
	s, m := storeFixture(t)
	stored := importTemplateFixture()
	stored.Version = 1
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("upstream_governance_import_templates").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT value FROM settings WHERE key=\\$1 FOR UPDATE").WithArgs("upstream_governance_import_templates").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(string(raw)))
	m.ExpectRollback()
	_, err = NewService(s, nil, nil, nil, false).SaveImportTemplates(t.Context(), importTemplateFixture())
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestImportTemplatesSQLFailedCommitDoesNotAdvanceCallerVersion(t *testing.T) {
	s, m := storeFixture(t)
	input := importTemplateFixture()
	failure := errors.New("fixture commit failure")
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("upstream_governance_import_templates").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT value FROM settings WHERE key=\\$1 FOR UPDATE").WithArgs("upstream_governance_import_templates").WillReturnError(sql.ErrNoRows)
	m.ExpectExec("INSERT INTO settings").WithArgs("upstream_governance_import_templates", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit().WillReturnError(failure)
	err := s.(*sqlStore).SaveImportTemplates(t.Context(), &input, 0)
	require.ErrorIs(t, err, failure)
	require.Equal(t, int64(0), input.Version)
	require.NoError(t, m.ExpectationsWereMet())
}
