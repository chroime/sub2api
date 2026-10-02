package migrations

import (
	"strings"
	"testing"
)

func TestPricingPolicyMigrationPersistsCostFactsAndOperations(t *testing.T) {
	sql, err := FS.ReadFile("263_upstream_governance_pricing_policies.sql")
	if err != nil {
		t.Fatal(err)
	}
	content := string(sql)
	for _, table := range []string{
		"upstream_governance_pricing_policies",
		"upstream_governance_pricing_cost_facts",
		"upstream_governance_pricing_operations",
	} {
		if !strings.Contains(content, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("migration does not create %s", table)
		}
	}
	for _, column := range []string{
		"baseline_cost",
		"baseline_sale",
		"decrease_stability_seconds",
		"max_increase_percent",
		"manual_owner",
		"protected",
		"cost_fact_revision",
	} {
		if !strings.Contains(content, column) {
			t.Fatalf("migration missing %s", column)
		}
	}
	if strings.Contains(strings.ToLower(content), "drop table") || strings.Contains(strings.ToLower(content), "drop column") {
		t.Fatal("pricing migration must be forward-only")
	}
}
