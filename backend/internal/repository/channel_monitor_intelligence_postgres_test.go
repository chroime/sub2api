//go:build unit

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Every table is temporary and search_path has no production fallback. A single
// connection keeps the fixtures private to this test, including on a local DB.
func monitorIntelligencePostgres(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SUB2API_TEST_POSTGRES_DSN to test local-group intelligence SQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`SET search_path=pg_temp;
SET statement_timeout='10s';
CREATE TEMP TABLE channel_monitors(id bigint,account_id bigint,provider text,group_name text,enabled boolean,
 group_id bigint,local_api_key_id bigint,group_binding_mode text DEFAULT 'manual',group_binding_changed_at timestamptz,
 primary_model text DEFAULT 'usage-model');
CREATE TEMP TABLE api_keys(id bigint,group_id bigint,deleted_at timestamptz);
CREATE TEMP TABLE groups(id bigint,name text,platform text,status text,deleted_at timestamptz);
CREATE TEMP TABLE upstream_governance_sites(id bigint,status text,login_cipher text,enabled boolean);
CREATE TEMP TABLE upstream_governance_model_policies(id bigint,site_id bigint,config jsonb,enabled boolean,version bigint,updated_at timestamptz);
CREATE TEMP TABLE upstream_governance_model_batches(id text,site_id bigint,policy_id bigint,policy_version bigint,config jsonb);
CREATE TEMP TABLE upstream_governance_model_runs(id text,batch_id text,site_id bigint,policy_id bigint,request jsonb,status text,result jsonb,review text,public_visible boolean,finished_at timestamptz);
-- Legacy account fixtures intentionally coexist, so using the wrong source is caught.
CREATE TEMP TABLE accounts(id bigint,status text,schedulable boolean,deleted_at timestamptz,expires_at timestamptz,platform text);
CREATE TEMP TABLE account_groups(account_id bigint,group_id bigint);
CREATE TEMP TABLE account_quality_policies(id bigint,enabled boolean,config jsonb,deleted_at timestamptz,updated_at timestamptz);
CREATE TEMP TABLE account_quality_bindings(id bigint,account_id bigint,policy_id bigint);
CREATE TEMP TABLE account_quality_runs(id text,account_id bigint,binding_id bigint,policy_id bigint,execution_status text,evidence jsonb,finished_at timestamptz);
INSERT INTO groups VALUES (1,'main','openai','active',NULL),(2,'account-only','openai','active',NULL);
INSERT INTO channel_monitors(id,account_id,provider,group_name,enabled,group_id) VALUES (1,NULL,'openai','main',TRUE,1),(2,NULL,'openai','account-only',TRUE,2);
INSERT INTO upstream_governance_sites VALUES (10,'local_model','local-model-owner:1',TRUE);
INSERT INTO upstream_governance_model_policies VALUES
 (100,10,'{"target_type":"local_group","local_group_id":1,"target_owner_user_id":1,"platform":"openai","model":"local-iq-model","efforts":["medium"],"templates":["candy","pelican"]}',TRUE,2,now());
INSERT INTO upstream_governance_model_batches SELECT 'current',site_id,id,version,config FROM upstream_governance_model_policies;
INSERT INTO upstream_governance_model_runs SELECT 'candy-pass','current',site_id,id,
 jsonb_build_object('config',config,'template','candy','effort','medium'),
 'succeeded','{"candy_verdict":"numeric_correct"}','pending',TRUE,'2026-11-14T11:59:20Z' FROM upstream_governance_model_policies;
INSERT INTO accounts VALUES (20,'active',TRUE,NULL,NULL,'openai');
INSERT INTO account_groups VALUES (20,2);
INSERT INTO account_quality_policies VALUES (200,TRUE,'{"model":"account-quality-model","effort":"high"}',NULL,now());
INSERT INTO account_quality_bindings VALUES (300,20,200);
INSERT INTO account_quality_runs VALUES ('account-pass',20,300,200,'completed','{"model":"account-quality-model","effort":"high","quality_verdict":"pass"}','2026-11-14T12:00:00Z');`)
	require.NoError(t, err)
	return db
}

func TestMonitorIntelligencePostgresReadsLocalGroupMonitoring(t *testing.T) {
	db := monitorIntelligencePostgres(t)
	now := time.Date(2026, 11, 14, 12, 0, 30, 0, time.UTC)
	result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), []int64{1, 2}, now)
	require.NoError(t, err)
	require.NotNil(t, result[1], "configured local-group intelligence must be projected")
	require.Nil(t, result[2], "account quality must not configure group intelligence")
	require.Len(t, result[1].Series, 1)
	require.Equal(t, "local-iq-model", result[1].Series[0].Model)
	require.Equal(t, "medium", result[1].Series[0].Effort)
	require.Equal(t, "pass", result[1].Series[0].Points[0].Status)
}

func TestMonitorIntelligencePostgresVerdicts(t *testing.T) {
	tests := []struct {
		name, template, status, review, result, want string
	}{
		{"correct candy", "candy", "succeeded", "pending", `{"candy_verdict":"numeric_correct"}`, "pass"},
		{"incorrect candy", "candy", "succeeded", "pending", `{"candy_verdict":"numeric_incorrect"}`, "fail"},
		{"ambiguous candy", "candy", "succeeded", "pending", `{"candy_verdict":"needs_review"}`, "pending"},
		{"missing verdict", "candy", "succeeded", "pending", `{}`, "pending"},
		{"HTTP success only", "candy", "succeeded", "pending", `{"success":true}`, "pending"},
		{"Pelican generated", "pelican", "succeeded", "pending", `{"success":true,"html":"<svg/>"}`, "pending"},
		{"Pelican reviewed pass", "pelican", "succeeded", "pass", `{}`, "pass"},
		{"Pelican reviewed fail", "pelican", "succeeded", "fail", `{}`, "fail"},
		{"human corrects automatic fail", "candy", "succeeded", "pass", `{"candy_verdict":"numeric_incorrect"}`, "pass"},
		{"human overrides automatic pass", "candy", "succeeded", "fail", `{"candy_verdict":"numeric_correct"}`, "fail"},
		{"execution failed", "pelican", "failed", "pending", `{}`, "error"},
		{"uncertain execution", "candy", "indeterminate", "pending", `{}`, "error"},
		{"review does not turn execution failure into success", "candy", "failed", "pass", `{}`, "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := monitorIntelligencePostgres(t)
			_, err := db.Exec(`UPDATE upstream_governance_model_runs SET request=jsonb_set(request,'{template}',to_jsonb($1::text)),status=$2,review=$3,result=$4::jsonb`, tt.template, tt.status, tt.review, tt.result)
			require.NoError(t, err)
			now := time.Date(2026, 11, 14, 12, 0, 30, 0, time.UTC)
			result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), []int64{1}, now)
			require.NoError(t, err)
			require.Len(t, result[1].Series[0].Points, 1)
			require.Equal(t, tt.want, result[1].Series[0].Points[0].Status)
		})
	}
}

func TestMonitorIntelligencePostgresScopeAndCurrentConfiguration(t *testing.T) {
	tests := []struct {
		name, mutation      string
		configured, history bool
	}{
		{"display name does not affect binding", `UPDATE channel_monitors SET group_name='Main'`, true, true},
		{"group rename preserves binding", `UPDATE groups SET name='renamed' WHERE id=1`, true, true},
		{"different group ID", `UPDATE channel_monitors SET group_id=2`, false, false},
		{"exact platform", `UPDATE channel_monitors SET provider='grok'`, false, false},
		{"disabled monitor", `UPDATE channel_monitors SET enabled=FALSE`, false, false},
		{"deleted group", `UPDATE groups SET deleted_at=now()`, false, false},
		{"inactive group", `UPDATE groups SET status='disabled'`, false, false},
		{"upstream target", `UPDATE upstream_governance_model_policies SET config=jsonb_set(config,'{target_type}','"upstream"')`, false, false},
		{"upstream workspace", `UPDATE upstream_governance_sites SET status='active'`, false, false},
		{"invalid workspace marker", `UPDATE upstream_governance_sites SET login_cipher='not-local'`, false, false},
		{"wrong owner", `UPDATE upstream_governance_sites SET login_cipher='local-model-owner:2'`, false, false},
		{"wrong policy platform", `UPDATE upstream_governance_model_policies SET config=jsonb_set(config,'{platform}','"grok"')`, false, false},
		{"probe-only policy", `UPDATE upstream_governance_model_policies SET config=jsonb_set(config,'{templates}','["probe"]')`, false, false},
		{"deleted policy", `DELETE FROM upstream_governance_model_policies`, false, false},
		{"paused policy retains configuration", `UPDATE upstream_governance_model_policies SET enabled=FALSE`, true, true},
		{"account monitor uses group IQ", `UPDATE channel_monitors SET account_id=999 WHERE id=1`, true, true},
		{"hidden run", `UPDATE upstream_governance_model_runs SET public_visible=FALSE`, true, false},
		{"cadence-only policy update preserves identical checks", `UPDATE upstream_governance_model_batches SET policy_version=1`, true, true},
		{"old version with changed evaluation config", `UPDATE upstream_governance_model_batches SET policy_version=1,config=config||'{"max_output_tokens":1024}'::jsonb`, true, false},
		{"future policy version", `UPDATE upstream_governance_model_batches SET policy_version=3`, true, false},
		{"wrong run model", `UPDATE upstream_governance_model_runs SET request=jsonb_set(request,'{config,model}','"different"')`, true, false},
		{"wrong effort", `UPDATE upstream_governance_model_runs SET request=jsonb_set(request,'{effort}','"low"')`, true, false},
		{"wrong run target", `UPDATE upstream_governance_model_runs SET request=jsonb_set(request,'{config,target_type}','"upstream"')`, true, false},
		{"wrong run group", `UPDATE upstream_governance_model_runs SET request=jsonb_set(request,'{config,local_group_id}','2')`, true, false},
		{"wrong run platform", `UPDATE upstream_governance_model_runs SET request=jsonb_set(request,'{config,platform}','"grok"')`, true, false},
		{"probe result", `UPDATE upstream_governance_model_runs SET request=jsonb_set(request,'{template}','"probe"')`, true, false},
		{"removed template", `UPDATE upstream_governance_model_policies SET config=jsonb_set(config,'{templates}','["pelican"]')`, true, false},
		{"historical checks remain visible", `UPDATE upstream_governance_model_runs SET finished_at='2026-11-12T11:00:59Z'`, true, true},
		{"future history", `UPDATE upstream_governance_model_runs SET finished_at='2026-11-14T12:00:31Z'`, true, false},
		{"unfinished", `UPDATE upstream_governance_model_runs SET finished_at=NULL`, true, false},
		{"queued", `UPDATE upstream_governance_model_runs SET status='queued'`, true, false},
		{"running", `UPDATE upstream_governance_model_runs SET status='running'`, true, false},
		{"cancelled", `UPDATE upstream_governance_model_runs SET status='cancelled'`, true, false},
		{"skipped", `UPDATE upstream_governance_model_runs SET status='skipped'`, true, false},
		{"wrong batch site", `UPDATE upstream_governance_model_batches SET site_id=999`, true, false},
		{"wrong batch policy", `UPDATE upstream_governance_model_batches SET policy_id=999`, true, false},
		{"wrong run site", `UPDATE upstream_governance_model_runs SET site_id=999`, true, false},
		{"wrong run policy", `UPDATE upstream_governance_model_runs SET policy_id=999`, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := monitorIntelligencePostgres(t)
			_, err := db.Exec(tt.mutation)
			require.NoError(t, err)
			now := time.Date(2026, 11, 14, 12, 0, 30, 0, time.UTC)
			result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), []int64{1, 2}, now)
			require.NoError(t, err)
			require.Nil(t, result[2], "account-quality data must always be ignored")
			if !tt.configured {
				require.Nil(t, result[1])
				return
			}
			require.NotNil(t, result[1])
			require.Len(t, result[1].Series, 1)
			if tt.history {
				require.Len(t, result[1].Series[0].Points, 1)
			} else {
				require.NotNil(t, result[1].Series[0].Points)
				require.Empty(t, result[1].Series[0].Points)
			}
		})
	}
}

func TestMonitorIntelligencePostgresKeepsDistinctSameMinuteChecks(t *testing.T) {
	for _, verdict := range []string{"numeric_incorrect", "needs_review", "numeric_correct", "error"} {
		t.Run(verdict, func(t *testing.T) {
			db := monitorIntelligencePostgres(t)
			_, err := db.Exec(`INSERT INTO upstream_governance_model_runs
SELECT 'second-sample',batch_id,site_id,policy_id,request,
 CASE WHEN $1='error' THEN 'failed' ELSE 'succeeded' END,
 jsonb_build_object('candy_verdict',$1::text),review,public_visible,finished_at FROM upstream_governance_model_runs`, verdict)
			require.NoError(t, err)
			now := time.Date(2026, 11, 14, 12, 0, 30, 0, time.UTC)
			result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), []int64{1}, now)
			require.NoError(t, err)
			require.Len(t, result[1].Series[0].Points, 2)
			expected := map[string]string{"numeric_incorrect": "fail", "needs_review": "pending", "numeric_correct": "pass", "error": "error"}[verdict]
			require.Equal(t, expected, result[1].Series[0].Points[0].Status)
			require.Equal(t, "pass", result[1].Series[0].Points[1].Status)
		})
	}
}

func TestMonitorIntelligencePostgresLimitsLatestActualChecksPerSeries(t *testing.T) {
	db := monitorIntelligencePostgres(t)
	_, err := db.Exec(`DELETE FROM upstream_governance_model_runs;
INSERT INTO upstream_governance_model_runs
 SELECT 'half-hour-'||lpad(n::text,3,'0'),b.id,b.site_id,b.policy_id,
 jsonb_build_object('config',b.config,'template','candy','effort','medium'),
 'succeeded','{"candy_verdict":"numeric_correct"}','pending',TRUE,
 '2026-11-14T12:00:00Z'::timestamptz-n*INTERVAL '30 minutes'
 FROM upstream_governance_model_batches b CROSS JOIN generate_series(0,64) n;
INSERT INTO upstream_governance_model_policies SELECT 101,site_id,jsonb_set(config,'{model}','"second-model"'),FALSE,1,now() FROM upstream_governance_model_policies;
INSERT INTO upstream_governance_model_batches SELECT 'second',site_id,id,version,config FROM upstream_governance_model_policies WHERE id=101;
INSERT INTO upstream_governance_model_runs
 SELECT 'second-model-run',id,site_id,policy_id,
 jsonb_build_object('config',config,'template','candy','effort','medium'),
 'succeeded','{"candy_verdict":"numeric_correct"}','pending',TRUE,'2026-11-10T12:00:00Z'
 FROM upstream_governance_model_batches WHERE id='second';`)
	require.NoError(t, err)
	now := time.Date(2026, 11, 14, 12, 0, 30, 0, time.UTC)
	result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	require.Len(t, result[1].Series, 2)
	points := result[1].Series[0].Points
	require.Len(t, points, 60, "bound each model/effort history instead of returning all history")
	require.True(t, now.Add(-30*time.Second).Equal(points[0].CheckedAt))
	require.True(t, now.Add(-30*time.Second-59*30*time.Minute).Equal(points[59].CheckedAt))
	require.Len(t, result[1].Series[1].Points, 1, "another model retains its own older history")
}

func TestMonitorIntelligencePostgresSeparatesTargetsWithoutInventingHistory(t *testing.T) {
	db := monitorIntelligencePostgres(t)
	_, err := db.Exec(`UPDATE upstream_governance_model_policies SET config=jsonb_set(config,'{efforts}','["medium","high"]');
INSERT INTO upstream_governance_model_policies SELECT 101,site_id,jsonb_set(config,'{model}','"second-model"'),FALSE,1,now() FROM upstream_governance_model_policies;
INSERT INTO upstream_governance_model_runs SELECT 'high-fail',batch_id,site_id,policy_id,jsonb_set(request,'{effort}','"high"'),status,'{"candy_verdict":"numeric_incorrect"}',review,public_visible,finished_at FROM upstream_governance_model_runs;`)
	require.NoError(t, err)
	now := time.Date(2026, 11, 14, 12, 0, 30, 0, time.UTC)
	result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	require.Len(t, result[1].Series, 4)
	for _, series := range result[1].Series {
		if series.Model == "second-model" {
			require.Empty(t, series.Points)
		} else {
			require.Len(t, series.Points, 1)
			expected := map[string]string{"medium": "pass", "high": "fail"}[series.Effort]
			require.Equal(t, expected, series.Points[0].Status)
		}
	}
}
