package upstreamgovernance

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSaveKeyHealthReportsIdentityConflict(t *testing.T) {
	store, mock := storeFixture(t)
	mock.ExpectExec(`UPDATE upstream_governance_keys SET key_health=`).WillReturnResult(sqlmock.NewResult(0, 0))
	key := ManagedKey{ID: 9, SiteID: 1, RemoteGroupID: "8", Platform: "openai", RemoteKeyID: "12", Marker: "stable", OwnerUserID: 5, KeyCipher: "encrypted"}
	checked := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	health := KeyHealth{Status: "present", LastCheckedAt: &checked, LastVerifiedAt: &checked}
	err := store.(KeyHealthStore).SaveKeyHealth(t.Context(), key, health)
	require.ErrorIs(t, err, ErrConflict)
}

func TestSaveKeyHealthRejectsInvalidObservationBeforeWrite(t *testing.T) {
	store, _ := storeFixture(t)
	key := ManagedKey{ID: 9, SiteID: 1, RemoteGroupID: "8", Platform: "openai", RemoteKeyID: "12", Marker: "stable", OwnerUserID: 5, KeyCipher: "encrypted"}
	err := store.(KeyHealthStore).SaveKeyHealth(t.Context(), key, KeyHealth{Status: "confirmed_missing", MissingCount: -1})
	require.ErrorIs(t, err, ErrInvalid)
}
