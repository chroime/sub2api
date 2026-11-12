package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupIdentityMigrationAddsOnlyUnconfiguredPresentation(t *testing.T) {
	content, err := FS.ReadFile("289_group_identity.sql")
	require.NoError(t, err)
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "--") {
			lines = append(lines, line)
		}
	}
	sql := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	require.Contains(t, sql, "ALTER TABLE groups ADD COLUMN IF NOT EXISTS identity JSONB NOT NULL DEFAULT '{}'::jsonb;")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE ", "existing groups must not receive inferred identities")
	require.NotContains(t, strings.ToUpper(sql), "INSERT ")
	require.NotContains(t, strings.ToUpper(sql), "DELETE ")
	require.NotContains(t, strings.ToUpper(sql), "DROP ")
	require.NotContains(t, sql, "name =")
	require.NotContains(t, sql, "platform =")
	require.NotContains(t, sql, "is_exclusive =")
}
