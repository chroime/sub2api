package migrations

import (
	"strings"
	"testing"
)

func TestLocalGroupChangesMigrationInstallsAtomicMutationCapture(t *testing.T) {
	raw, err := FS.ReadFile("273_upstream_governance_local_group_changes.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"create table if not exists upstream_governance_local_group_changes",
		"create or replace function record_upstream_governance_local_group_change()",
		"create trigger upstream_governance_local_group_change_trigger",
		"after insert or update or delete on groups",
		"to_jsonb(old)",
		"to_jsonb(new)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
	if strings.Contains(sql, "drop table") || strings.Contains(sql, "drop column") {
		t.Fatal("local group change migration must be forward-only")
	}
}
