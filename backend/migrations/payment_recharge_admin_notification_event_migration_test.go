package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentRechargeAdminNotificationEventMigrationOnlyExpandsAuditEvents(t *testing.T) {
	content, err := FS.ReadFile("288_payment_recharge_admin_notification_event.sql")
	require.NoError(t, err)
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "--") {
			lines = append(lines, line)
		}
	}
	sql := strings.Join(lines, " ")
	require.Equal(t, 2, strings.Count(sql, ";"), "only the existing event check is replaced")
	require.Contains(t, sql, "ALTER TABLE user_balance_notification_records DROP CONSTRAINT user_balance_notification_records_event_type_check;")
	require.Contains(t, sql, "ADD CONSTRAINT user_balance_notification_records_event_type_check CHECK (event_type IN ('balance.low', 'system.health_snapshot', 'payment.recharge_success_admin'));")
	for _, forbidden := range []string{"INSERT ", "UPDATE ", "DELETE ", "DROP TABLE", "DROP COLUMN", "ALTER COLUMN", "DEFAULT"} {
		require.NotContains(t, strings.ToUpper(sql), forbidden, "existing audit history and defaults must remain unchanged")
	}
}
