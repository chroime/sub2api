package upstreamgovernance

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectPricingAdminLoad(mock sqlmock.Sqlmock, state pricingAdminState, lock bool) {
	p := state.State.Policy
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	mock.ExpectQuery("SELECT .* FROM upstream_governance_pricing_policies WHERE local_group_id=\\$1" + suffix).WithArgs(p.LocalGroupID).WillReturnRows(sqlmock.NewRows(strings.Split(pricingPolicyColumns, ",")).AddRow(p.LocalGroupID, p.Enabled, p.Mode, p.BaselineCost, p.BaselineSale, p.Ratio, p.MinMargin, p.SafetyBuffer, p.DecreaseStabilitySeconds, p.MaxIncreasePercent, p.Version, p.ManualOwner, p.ManualVersion, p.LastAutomaticSale, p.LastAutomaticCost, p.ActiveCost, p.ActiveCostSource, p.Protected, p.ProtectionReason, p.CostFactRevision, nil))
	mock.ExpectQuery("SELECT rate_multiplier FROM groups WHERE id=\\$1 AND deleted_at IS NULL" + suffix).WithArgs(p.LocalGroupID).WillReturnRows(sqlmock.NewRows([]string{"rate_multiplier"}).AddRow(state.State.CurrentSale))
	bindings := sqlmock.NewRows([]string{"site_id", "site_name", "binding_id", "remote_group_id", "account_id"})
	for _, b := range state.Bindings {
		bindings.AddRow(b.SiteID, b.SiteName, b.BindingID, b.RemoteGroupID, b.AccountID)
	}
	mock.ExpectQuery("SELECT b.site_id,s.name,b.id,b.remote_group_id,b.account_id FROM upstream_governance_bindings").WithArgs(p.LocalGroupID).WillReturnRows(bindings)
	facts := sqlmock.NewRows([]string{"source_id", "site_id", "binding_id", "cost", "unit", "currency", "comparable", "eligible", "unknown", "observed_at"})
	for _, f := range state.State.Observations {
		facts.AddRow(f.SourceID, f.SiteID, f.BindingID, f.Cost, f.Unit, f.Currency, f.Comparable, f.Eligible, f.Unknown, f.ObservedAt)
	}
	mock.ExpectQuery("SELECT source_id,COALESCE.* FROM upstream_governance_pricing_cost_facts").WithArgs(p.LocalGroupID).WillReturnRows(facts)
}

func expectPricingConfigEmpty(mock sqlmock.Sqlmock, siteID int64, version int64) {
	mock.ExpectQuery("SELECT DISTINCT g.id,g.name FROM groups").WithArgs(siteID).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
	mock.ExpectQuery("SELECT version,policy FROM upstream_governance_pricing_notifications").WithArgs(siteID).WillReturnRows(sqlmock.NewRows([]string{"version", "policy"}).AddRow(version, `{"enabled":false,"recipients":[]}`))
}

func TestPricingAdminSQLPreviewNeverWrites(t *testing.T) {
	s, mock := storeFixture(t)
	draft, state := pricingAdminFixture()
	mock.ExpectBegin()
	expectPricingAdminLoad(mock, state, false)
	mock.ExpectCommit()
	preview, err := s.(*sqlStore).PreviewPricingPolicy(context.Background(), 1, 12, draft)
	require.NoError(t, err)
	require.Equal(t, .3385, *preview.TargetSale)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPricingAdminSQLSaveRejectsStaleFingerprintWithoutWrites(t *testing.T) {
	s, mock := storeFixture(t)
	draft, state := pricingAdminFixture()
	preview, _, err := preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	state.State.Observations[0].Cost = .25
	mock.ExpectBegin()
	expectPricingAdminLoad(mock, state, true)
	mock.ExpectRollback()
	_, err = s.(*sqlStore).SavePricingPolicy(context.Background(), 1, 12, draft, preview.Fingerprint)
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPricingAdminSQLSaveWritesOnlyEditablePolicy(t *testing.T) {
	s, mock := storeFixture(t)
	draft, state := pricingAdminFixture()
	state.State.Policy.ManualOwner = true
	state.State.Policy.LastAutomaticCost = .21
	state.State.Policy.ActiveCost = .22
	state.State.Policy.Protected = true
	state.State.Policy.ProtectionReason = "manual_owner"
	preview, _, err := preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	mock.ExpectBegin()
	expectPricingAdminLoad(mock, state, true)
	// No groups, notifications, ownership or runtime fields are in this write.
	mock.ExpectExec("UPDATE upstream_governance_pricing_policies SET enabled=\\$2,mode=\\$3,min_margin=\\$4,safety_buffer=\\$5,decrease_stability_seconds=\\$6,max_increase_percent=\\$7,baseline_cost=\\$8,baseline_sale=\\$9,ratio=\\$10,version=version\\+1,updated_at=NOW\\(\\) WHERE local_group_id=\\$1 AND version=\\$11").WithArgs(int64(12), true, PricingModeTargetMargin, .25, .1, int64(60), float64(20), .22, .3, float64(0), int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectPricingConfigEmpty(mock, 1, 8)
	_, err = s.(*sqlStore).SavePricingPolicy(context.Background(), 1, 12, draft, preview.Fingerprint)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPricingAdminSQLSaveRejectsUnboundGroup(t *testing.T) {
	s, mock := storeFixture(t)
	draft, state := pricingAdminFixture()
	mock.ExpectBegin()
	expectPricingAdminLoad(mock, state, true)
	mock.ExpectRollback()
	_, err := s.(*sqlStore).SavePricingPolicy(context.Background(), 2, 12, draft, strings.Repeat("a", 64))
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPricingAdminSQLNotificationsIsolatedAndVersionChecked(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved", true: "stale"}[stale], func(t *testing.T) {
			s, mock := storeFixture(t)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT id FROM upstream_governance_sites WHERE id=\\$1 FOR UPDATE").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectQuery("SELECT version FROM upstream_governance_pricing_notifications WHERE site_id=\\$1 FOR UPDATE").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(3))
			version := int64(3)
			if stale {
				version = 2
				mock.ExpectRollback()
			} else {
				mock.ExpectExec("INSERT INTO upstream_governance_pricing_notifications").WithArgs(int64(1), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
				expectPricingConfigEmpty(mock, 1, 4)
			}
			_, err := s.(*sqlStore).SavePricingNotifications(context.Background(), 1, version, PricingNotificationPolicy{Recipients: []string{}})
			if stale {
				require.ErrorIs(t, err, ErrConflict)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPricingAdminSQLNotificationsMissingSiteDoesNotInsert(t *testing.T) {
	s, mock := storeFixture(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM upstream_governance_sites").WithArgs(int64(1)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	_, err := s.(*sqlStore).SavePricingNotifications(context.Background(), 1, 1, PricingNotificationPolicy{})
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPricingAdminLegacyBatchRejectsUnboundGroupBeforeWriting(t *testing.T) {
	s, mock := storeFixture(t)
	draft, state := pricingAdminFixture()
	p := state.State.Policy
	p.Enabled, p.Mode, p.MinMargin, p.SafetyBuffer = draft.Enabled, draft.Mode, draft.MinMargin, draft.SafetyBuffer
	mock.ExpectBegin()
	expectPricingAdminLoad(mock, state, true)
	mock.ExpectRollback()
	_, err := s.(*sqlStore).SavePricingPolicies(context.Background(), 2, PricingPoliciesConfiguration{Version: 1, Policies: []PricingPolicyView{{PricingPolicy: p}}})
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPricingAdminLegacyBatchRejectsStalePolicyVersion(t *testing.T) {
	s, mock := storeFixture(t)
	_, state := pricingAdminFixture()
	p := state.State.Policy
	p.Version--
	mock.ExpectBegin()
	expectPricingAdminLoad(mock, state, true)
	mock.ExpectRollback()
	_, err := s.(*sqlStore).SavePricingPolicies(context.Background(), 1, PricingPoliciesConfiguration{Version: 1, Policies: []PricingPolicyView{{PricingPolicy: p}}})
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPricingAdminAbsentPolicyUsesDatabaseProtectionDefaults(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "persisted_zero_is_intentional"}[persisted], func(t *testing.T) {
			s, mock := storeFixture(t)
			mock.ExpectQuery("SELECT rate_multiplier FROM groups").WithArgs(int64(12)).WillReturnRows(sqlmock.NewRows([]string{"rate_multiplier"}).AddRow(.3))
			policyRows := sqlmock.NewRows(strings.Split(pricingPolicyColumns, ","))
			if persisted {
				policyRows.AddRow(12, false, "keep_margin", 0, 0, 0, 0, 0, 0, 0, 1, false, 0, 0, 0, 0, "", false, "", 0, nil)
			}
			mock.ExpectQuery("SELECT .* FROM upstream_governance_pricing_policies").WithArgs(int64(12)).WillReturnRows(policyRows)
			mock.ExpectQuery("SELECT source_id,cost,unit,currency,comparable,eligible,unknown,observed_at FROM upstream_governance_pricing_cost_facts").WithArgs(int64(12)).WillReturnRows(sqlmock.NewRows([]string{"source_id", "cost", "unit", "currency", "comparable", "eligible", "unknown", "observed_at"}))
			state, err := s.(*sqlStore).LoadPricingState(context.Background(), 12)
			require.NoError(t, err)
			if persisted {
				require.Zero(t, state.Policy.DecreaseStabilitySeconds)
				require.Zero(t, state.Policy.MaxIncreasePercent)
			} else {
				require.Equal(t, int64(60), state.Policy.DecreaseStabilitySeconds)
				require.Equal(t, float64(20), state.Policy.MaxIncreasePercent)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
