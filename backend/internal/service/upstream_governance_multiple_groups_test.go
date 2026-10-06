package service

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceNativeMultiGroupAndPriorityRecovery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	admin := &governanceAdminStub{groups: map[int64]*Group{
		3: {ID: 3, Platform: PlatformOpenAI, Status: StatusActive},
		4: {ID: 4, Platform: PlatformComposite, Status: StatusActive},
	}}
	local := &governanceLocalAccounts{db: db, admin: admin}
	change := gov.AccountChange{Marker: "multi-marker", Name: "Imported", Platform: PlatformOpenAI, BaseURL: "https://fixture.example", APIKey: "fixture-key", GroupIDs: []int64{4, 3, 4}, CostMultiplier: 1}
	mock.ExpectQuery("SELECT id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, err = local.ApplyAccount(t.Context(), change)
	require.NoError(t, err)
	require.Equal(t, []int64{3, 4}, admin.account.GroupIDs)
	require.Equal(t, 1, admin.account.Priority)
	admin.account.GroupIDs = []int64{4, 3}
	mock.ExpectQuery("SELECT id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(admin.account.ID))
	_, err = local.ApplyAccount(t.Context(), change)
	require.NoError(t, err)
	require.Zero(t, admin.updates, "same set in another order must be recovered without a write")
	change.ExpectedFingerprint = governanceLocal(admin.account).Fingerprint
	change.GroupIDs = []int64{3}
	change.AccountConfig, err = gov.NormalizeAccountConfig(nil, PlatformOpenAI, nil)
	require.NoError(t, err)
	zero := 0
	change.AccountConfig.Priority = &zero
	mock.ExpectQuery("SELECT id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(admin.account.ID))
	_, err = local.ApplyAccount(t.Context(), change)
	require.NoError(t, err)
	require.Equal(t, []int64{3}, admin.account.GroupIDs)
	require.Zero(t, admin.account.Priority)
	require.Equal(t, 1, admin.updates)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGovernanceNativeRechecksEveryTargetAndAccountPriority(t *testing.T) {
	admin := &governanceAdminStub{groups: map[int64]*Group{
		3: {ID: 3, Platform: PlatformOpenAI, Status: StatusActive},
		4: {ID: 4, Platform: PlatformComposite, Status: StatusActive},
	}}
	local := &governanceLocalAccounts{admin: admin}
	targets := map[int64]string{}
	for _, id := range []int64{3, 4} {
		target, err := local.Target(t.Context(), id, PlatformOpenAI)
		require.NoError(t, err)
		targets[id] = target.Fingerprint
	}
	admin.groups[4].RateMultiplier = 2
	_, err := local.ApplyAccount(t.Context(), gov.AccountChange{Marker: "marker", APIKey: "key", Platform: PlatformOpenAI, GroupIDs: []int64{3, 4}, ExpectedTargetFingerprints: targets})
	require.ErrorIs(t, err, gov.ErrConflict)
	require.Zero(t, admin.creates)
	require.Zero(t, admin.updates)
	account := &Account{Priority: 7}
	before := GovernanceAccountFingerprint(account)
	account.Priority = 1
	require.NotEqual(t, before, GovernanceAccountFingerprint(account))
}
