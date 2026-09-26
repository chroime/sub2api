package repository

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGovernanceLockedFingerprintRejectsConcurrentAccountEdits(t *testing.T) {
	for _, field := range []string{"same", "credentials", "proxy", "groups", "extra", "name", "rate", "parent"} {
		t.Run(field, func(t *testing.T) {
			db, m, e := sqlmock.New()
			require.NoError(t, e)
			defer db.Close()
			rate := 2.0
			a := &service.Account{ID: 9, Name: "import", Platform: "openai", Type: "apikey", Status: "active", Credentials: map[string]any{"api_key": "old"}, Extra: map[string]any{"upstream_governance_marker": "marker", "unrelated": "preserved"}, GroupIDs: []int64{3}, RateMultiplier: &rate}
			expected := service.GovernanceAccountFingerprint(a)
			switch field {
			case "credentials":
				a.Credentials["api_key"] = "admin-edit"
			case "parent":
				id := int64(11)
				a.ParentAccountID = &id
			case "proxy":
				id := int64(7)
				a.ProxyID = &id
			case "groups":
				a.GroupIDs = []int64{5}
			case "extra":
				a.Extra["unrelated"] = "admin-edit"
			case "name":
				a.Name = "admin-edit"
			case "rate":
				rate = 3
			}
			creds, _ := json.Marshal(a.Credentials)
			extra, _ := json.Marshal(a.Extra)
			var proxy, parent any
			if a.ParentAccountID != nil {
				parent = *a.ParentAccountID
			}
			if a.ProxyID != nil {
				proxy = *a.ProxyID
			}
			m.ExpectQuery("SELECT name,platform,type,status,credentials,extra,proxy_id,rate_multiplier,parent_account_id.*FOR UPDATE").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"name", "platform", "type", "status", "credentials", "extra", "proxy", "rate", "parent"}).AddRow(a.Name, a.Platform, a.Type, a.Status, creds, extra, proxy, rate, parent))
			m.ExpectQuery("SELECT group_id FROM account_groups").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(a.GroupIDs[0]))
			e = checkGovernanceAccountCAS(context.Background(), db, 9, expected)
			if field == "same" {
				require.NoError(t, e)
			} else {
				require.ErrorIs(t, e, gov.ErrConflict)
			}
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}
