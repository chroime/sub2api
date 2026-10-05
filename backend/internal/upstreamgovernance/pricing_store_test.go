package upstreamgovernance

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestScanPricingPolicyAllowsUnsetDecreaseObservation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("pricing_policy").WillReturnRows(sqlmock.NewRows([]string{
		"local_group_id", "enabled", "mode", "baseline_cost", "baseline_sale", "ratio", "min_margin", "safety_buffer",
		"decrease_stability_seconds", "max_increase_percent", "version", "manual_owner", "manual_version",
		"last_automatic_sale", "last_automatic_cost", "active_cost", "active_cost_source", "protected", "protection_reason",
		"cost_fact_revision", "decrease_observed_at",
	}).AddRow(1, false, "keep_margin", 0, 0, 0, 0, 0, 60, 20, 1, false, 0, 0, 0, 0, "", false, "", 0, nil))
	policy, err := scanPricingPolicy(db.QueryRow("SELECT pricing_policy"))
	require.NoError(t, err)
	require.Equal(t, int64(1), policy.LocalGroupID)
	require.True(t, policy.DecreaseObservedAt.IsZero())
	require.NoError(t, mock.ExpectationsWereMet())
}
