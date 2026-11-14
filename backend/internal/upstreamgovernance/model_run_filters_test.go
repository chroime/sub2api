package upstreamgovernance

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelRunFiltersPostgres(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_RUN_FILTER_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_RUN_FILTER_TEST_DSN to a loopback PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("model_run_filters_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close(); base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); base.Close() })
	_, err = db.Exec(`CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,deleted_at TIMESTAMPTZ);
CREATE TABLE upstream_governance_keys(id BIGINT PRIMARY KEY,site_id BIGINT,remote_group_id TEXT,platform TEXT);
CREATE TABLE upstream_governance_snapshots(id BIGINT PRIMARY KEY,site_id BIGINT,catalog JSONB);
CREATE TABLE upstream_governance_model_runs(id TEXT PRIMARY KEY,batch_id TEXT,site_id BIGINT,policy_id BIGINT,sequence INT,request JSONB,status TEXT,result JSONB,review TEXT,review_note TEXT,created_at TIMESTAMPTZ,started_at TIMESTAMPTZ,finished_at TIMESTAMPTZ,reviewer_id BIGINT,reviewed_at TIMESTAMPTZ,review_version BIGINT);
INSERT INTO groups VALUES(12,'Claude 50%_ literal',NULL);
INSERT INTO upstream_governance_keys VALUES(17,7,'g1','openai'),(18,8,'g1','openai');
INSERT INTO upstream_governance_snapshots VALUES(1,7,'{"groups":[{"id":"g1","name":"Old upstream","platform":"openai"}]}'),(2,7,'{"groups":[{"id":"g1","name":"Current upstream","platform":"openai"}]}'),(3,8,'{"groups":[{"id":"g1","name":"Other site secret","platform":"openai"}]}');`)
	require.NoError(t, err)
	anchor := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 32; i++ {
		config := ModelTestConfig{TargetType: "local_group", LocalGroupID: 12, Model: "gpt-fixture", Platform: "openai"}
		status, review, effort, template, site := "succeeded", "pending", "high", "candy", int64(7)
		if i >= 25 {
			status = "running"
		}
		if i == 0 {
			status = "failed"
			review = "fail"
			effort = "low"
			template = "pelican"
		}
		if i == 30 {
			config = ModelTestConfig{ManagedKeyID: 17, Model: "gpt-upstream", Platform: "openai"}
		}
		if i == 31 {
			site = 8
			config.ManagedKeyID = 18
		}
		req, _ := json.Marshal(ModelRunRequest{Config: config, Template: template, Effort: effort, Sample: i + 1})
		_, err = db.Exec(`INSERT INTO upstream_governance_model_runs VALUES($1,'batch-one',$2,NULL,$3,$4,$5,'{"response_text":"large private evidence","duration_ms":1000}',$6,'',$7,NULL,NULL,NULL,NULL,1)`, fmt.Sprintf("run-%02d", i), site, i, req, status, review, anchor.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
	}
	m := &modelStore{db: db}
	check := func(filters ModelRunFilters, total int64) *ModelRunPage {
		t.Helper()
		p, err := m.listRuns(t.Context(), 7, 1, 20, "batch-one", filters)
		require.NoError(t, err)
		require.Equal(t, total, p.Total)
		require.EqualValues(t, 31, p.ScopeTotal)
		require.EqualValues(t, 6, p.Counts["running"])
		for _, r := range p.Items {
			require.EqualValues(t, 7, r.SiteID)
			require.Empty(t, r.Result.ResponseText)
		}
		return p
	}
	check(ModelRunFilters{Status: "failed", Template: "pelican", Effort: "low", Review: "fail"}, 1)
	check(ModelRunFilters{Query: "Claude"}, 30)
	check(ModelRunFilters{Query: "50%_"}, 30)
	check(ModelRunFilters{Query: "50__"}, 0)
	check(ModelRunFilters{Query: "Current upstream"}, 1)
	check(ModelRunFilters{Query: "Old upstream"}, 0)
	check(ModelRunFilters{Query: "Other site secret"}, 0)
	check(ModelRunFilters{Query: "batch-one"}, 31)
	check(ModelRunFilters{Query: "gpt-upstream"}, 1)
	check(ModelRunFilters{Query: "' OR 1=1 --"}, 0)
	from, to := anchor.Add(5*time.Minute), anchor.Add(10*time.Minute)
	check(ModelRunFilters{CreatedFrom: &from, CreatedTo: &to}, 5)
	p, err := m.listRuns(t.Context(), 7, 2, 20, "", ModelRunFilters{Query: "Claude", Review: "pending"})
	require.NoError(t, err)
	require.EqualValues(t, 29, p.Total)
	require.Len(t, p.Items, 9)
	for _, r := range p.Items {
		require.EqualValues(t, 12, r.Request.Config.LocalGroupID)
		require.Equal(t, "pending", r.Review)
	}
}

func TestModelRunFiltersValidation(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	for _, f := range []ModelRunFilters{{Status: "bogus"}, {Template: "bogus"}, {Effort: "bogus"}, {Review: "bogus"}, {Query: strings.Repeat("界", 201)}, {CreatedFrom: &now, CreatedTo: &earlier}} {
		require.ErrorIs(t, f.Validate(), ErrInvalid)
	}
	for _, f := range []ModelRunFilters{{}, {Status: "indeterminate", Template: "token_audit", Effort: "default", Review: "pending"}, {CreatedFrom: &earlier, CreatedTo: &now}} {
		require.NoError(t, f.Validate())
	}
}
