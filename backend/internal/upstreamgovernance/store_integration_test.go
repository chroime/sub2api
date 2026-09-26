package upstreamgovernance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in against a throwaway, loopback fixture only. The test creates and drops
// its own schema, and does not inspect application configuration or credentials.
func TestSQLStorePostgresIntegration(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_STORE_TEST_DSN to an isolated loopback PostgreSQL fixture")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.User == nil || u.User.Username() != "governance_fixture" {
		t.Fatal("requires isolated loopback governance_fixture database")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	schema := fmt.Sprintf("governance_fixture_%d", time.Now().UnixNano())
	if _, e = db.Exec(`CREATE SCHEMA ` + schema); e != nil {
		t.Fatal(e)
	}
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	fixture, e := sql.Open("postgres", u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer fixture.Close()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, e := fixture.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	mustExec(`CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY); CREATE TABLE groups(id BIGSERIAL PRIMARY KEY); CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY, extra JSONB NOT NULL DEFAULT '{}', deleted_at TIMESTAMPTZ); INSERT INTO groups(id) VALUES (1);`)
	migration, e := os.ReadFile("../../migrations/247_upstream_governance.sql")
	if e != nil {
		t.Fatal(e)
	}
	mustExec(string(migration))
	ctx := context.Background()
	s := NewSQLStore(fixture)
	now := time.Now().UTC().Truncate(time.Microsecond)
	mustExec(`INSERT INTO proxies(id) VALUES (1)`)
	proxyID := int64(1)
	site := &Site{Name: "Fixture", Platform: "newapi", BaseURL: "https://fixture.example", ProxyID: &proxyID, Enabled: true, IntervalMinutes: 15, SessionCipher: "cipher-session", Status: "connected", NextSyncAt: now}
	if e = s.CreateSite(ctx, site); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetSite(ctx, site.ID)
	if e != nil || got.SessionCipher != "cipher-session" || !got.HasCredential || got.Version != 1 {
		t.Fatalf("site=%#v err=%v", got, e)
	}
	if _, e = fixture.Exec(`DELETE FROM proxies WHERE id=1`); e == nil {
		t.Fatal("proxy deletion must not silently switch governance to direct access")
	}
	got, e = s.GetSite(ctx, site.ID)
	if e != nil || got.ProxyID == nil || *got.ProxyID != 1 {
		t.Fatalf("proxy binding changed: %#v %v", got, e)
	}
	got.Name = "Updated"
	if e = s.UpdateSite(ctx, got, 1); e != nil || got.Version != 2 {
		t.Fatalf("update %v %#v", e, got)
	}
	if e = s.UpdateSite(ctx, site, 1); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale write: %v", e)
	}
	if e = s.ObserveSite(ctx, site.ID, "connected", "", now, now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	got, e = s.GetSite(ctx, site.ID)
	if e != nil || got.Version != 2 || !got.LastSyncAt.Equal(now) {
		t.Fatalf("observation changed version: %#v %v", got, e)
	}
	release, locked, e := s.LockSite(ctx, site.ID)
	if e != nil || !locked {
		t.Fatal(e)
	}
	_, locked, e = s.LockSite(ctx, site.ID)
	if e != nil || locked {
		t.Fatalf("lock contention %v %v", locked, e)
	}
	release()
	release, locked, e = s.LockSite(ctx, site.ID)
	if e != nil || !locked {
		t.Fatal(e)
	}
	release()
	base, user, resolved := 2.0, 0.8, 0.8
	snapshot := &Snapshot{SiteID: site.ID, SiteVersion: 2, Catalog: Catalog{Groups: []RemoteGroup{{ID: "g", RateMultiplier: &base, UserRateMultiplier: &user, ResolvedRateMultiplier: &resolved, Models: []string{"text"}, Prices: []RemotePrice{{Model: "text", Unit: "USD/M"}}}}, Channels: []RemoteChannel{{Name: "channel"}}, Warnings: []string{"unknown price"}}}
	if e = s.SaveSnapshot(ctx, snapshot, []Event{{Kind: "group_added", Resource: "g"}}); e != nil {
		t.Fatal(e)
	}
	latest, e := s.LatestSnapshot(ctx, site.ID)
	if e != nil || latest.SiteVersion != 2 || latest.Catalog.Groups[0].Prices[0].Input != nil || *latest.Catalog.Groups[0].ResolvedRateMultiplier != 0.8 {
		t.Fatalf("snapshot %#v %v", latest, e)
	}
	// Force event failure to prove the new catalog and its events cannot separate.
	mustExec(`ALTER TABLE upstream_governance_events ADD CONSTRAINT fixture_reject CHECK(kind <> 'reject')`)
	if e = s.SaveSnapshot(ctx, &Snapshot{SiteID: site.ID, SiteVersion: 2}, []Event{{Kind: "reject"}}); e == nil {
		t.Fatal("expected transaction failure")
	}
	latest, e = s.LatestSnapshot(ctx, site.ID)
	if e != nil || latest.ID != snapshot.ID {
		t.Fatal("failed discovery replaced catalog")
	}
	preview := &Preview{ID: "preview", SiteID: site.ID, SiteVersion: 2, SnapshotID: snapshot.ID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), Rows: []PreviewRow{{Marker: "stable"}}}
	if e = s.SavePreview(ctx, preview); e != nil {
		t.Fatal(e)
	}
	preview.Rows[0].Marker = "modified"
	if e = s.SavePreview(ctx, preview); !errors.Is(e, ErrConflict) {
		t.Fatalf("preview replace %v", e)
	}
	result := &ApplyResult{PreviewID: "preview", Items: []ItemResult{{Status: "success", AccountID: 8}}}
	if e = s.SavePreviewResult(ctx, site.ID, "preview", result); e != nil {
		t.Fatal(e)
	}
	frozen, e := s.GetPreview(ctx, site.ID, "preview")
	if e != nil || frozen.Rows[0].Marker != "stable" || frozen.Result.Items[0].AccountID != 8 {
		t.Fatalf("preview %#v %v", frozen, e)
	}
	binding := &Binding{SiteID: site.ID, RemoteGroupID: "g", Platform: "openai", LocalGroupID: 1, Marker: "stable", KeyCipher: "encrypted-key", ProbeEnabled: true, ProbeModel: "text", ProbeIntervalMinutes: 30, NextProbeAt: now}
	if e = s.SaveBinding(ctx, binding); e != nil {
		t.Fatal(e)
	}
	originalID := binding.ID
	binding.AccountID = 8
	if e = s.SaveBinding(ctx, binding); e != nil || binding.ID != originalID {
		t.Fatalf("binding duplication %v", e)
	}
	bindings, e := s.ListBindings(ctx, site.ID)
	if e != nil || len(bindings) != 1 || bindings[0].KeyCipher != "encrypted-key" || bindings[0].ProbeModel != "text" {
		t.Fatalf("binding roundtrip %#v %v", bindings, e)
	}
	due, e := s.DueSites(ctx, now, 20)
	if e != nil || len(due) != 1 {
		t.Fatalf("due probes %#v %v", due, e)
	}
	// Disconnected discovery entries cannot consume the whole batch, while
	// an existing inference key remains probeable without a dashboard session.
	for i := 0; i < 21; i++ {
		disconnected := &Site{Name: fmt.Sprintf("Disconnected %d", i), Platform: "newapi", BaseURL: "https://fixture.example", Enabled: true, IntervalMinutes: 15, Status: "disconnected", NextSyncAt: now.Add(-time.Hour)}
		if e = s.CreateSite(ctx, disconnected); e != nil {
			t.Fatal(e)
		}
	}
	connected := &Site{Name: "Connected discovery", Platform: "newapi", BaseURL: "https://fixture.example", Enabled: true, IntervalMinutes: 15, SessionCipher: "connected-cipher", Status: "connected", NextSyncAt: now}
	if e = s.CreateSite(ctx, connected); e != nil {
		t.Fatal(e)
	}
	probeOnly := &Site{Name: "Probe without session", Platform: "newapi", BaseURL: "https://fixture.example", Enabled: true, IntervalMinutes: 15, Status: "disconnected", NextSyncAt: now.Add(-time.Hour)}
	if e = s.CreateSite(ctx, probeOnly); e != nil {
		t.Fatal(e)
	}
	probeOnlyBinding := &Binding{SiteID: probeOnly.ID, RemoteGroupID: "probe-only", Platform: "openai", LocalGroupID: 1, AccountID: 9, Marker: "probe-only", KeyCipher: "probe-cipher", ProbeEnabled: true, ProbeModel: "text", ProbeIntervalMinutes: 30, NextProbeAt: now}
	if e = s.SaveBinding(ctx, probeOnlyBinding); e != nil {
		t.Fatal(e)
	}
	due, e = s.DueSites(ctx, now, 20)
	if e != nil || len(due) != 3 {
		t.Fatalf("disconnected entries starve eligible work: %#v %v", due, e)
	}
	expectedDue := map[int64]bool{site.ID: true, connected.ID: true, probeOnly.ID: true}
	for _, v := range due {
		if !expectedDue[v.ID] {
			t.Fatalf("unexpected due site %d", v.ID)
		}
		delete(expectedDue, v.ID)
	}
	if len(expectedDue) != 0 {
		t.Fatalf("eligible sites missing: %v", expectedDue)
	}
	if e = s.DeleteSite(ctx, site.ID); !errors.Is(e, ErrConflict) {
		t.Fatalf("deletion with binding: %v", e)
	}
	check := &Check{SiteID: site.ID, BindingID: binding.ID, Model: "text", ProbeResult: ProbeResult{Success: true, LatencyMS: 15}}
	if e = s.AddCheck(ctx, check); e != nil {
		t.Fatal(e)
	}
	checks, total, e := s.ListChecks(ctx, site.ID, 1, 100)
	if e != nil || total != 1 || !checks[0].Success {
		t.Fatalf("checks %#v %d %v", checks, total, e)
	}
	if v, e := s.LatestCheck(ctx, site.ID, binding.ID); e != nil || v.ID != check.ID || !v.Success {
		t.Fatalf("latest check %#v %v", v, e)
	}
	if v, e := s.LatestCheck(ctx, probeOnly.ID, binding.ID); v != nil || !errors.Is(e, ErrNotFound) {
		t.Fatalf("check leaked across sites %#v %v", v, e)
	}
	if v, e := s.LatestCheck(ctx, probeOnly.ID, probeOnlyBinding.ID); v != nil || !errors.Is(e, ErrNotFound) {
		t.Fatalf("missing check %#v %v", v, e)
	}
	failure := &Check{SiteID: site.ID, BindingID: binding.ID, Model: "text", ProbeResult: ProbeResult{Success: false, LatencyMS: 21, ErrorCode: "rate_limited"}}
	if e = s.AddCheck(ctx, failure); e != nil {
		t.Fatal(e)
	}
	if v, e := s.LatestCheck(ctx, site.ID, binding.ID); e != nil || v.ID != failure.ID || v.Success || v.ErrorCode != "rate_limited" {
		t.Fatalf("latest failure %#v %v", v, e)
	}
	events, total, e := s.ListEvents(ctx, site.ID, 1, 100)
	if e != nil || total != 1 {
		t.Fatalf("events %d %v", total, e)
	}
	if e = s.AckEvent(ctx, site.ID, events[0].ID); e != nil {
		t.Fatal(e)
	}
	events, _, e = s.ListEvents(ctx, site.ID, 1, 100)
	if e != nil || !events[0].Acknowledged {
		t.Fatal("ack not persisted")
	}
	mustExec(`INSERT INTO accounts(extra) VALUES ('{"upstream_governance_marker":"stable"}')`)
	if _, e = fixture.Exec(`INSERT INTO accounts(extra) VALUES ('{"upstream_governance_marker":"stable"}')`); e == nil || !strings.Contains(e.Error(), "unique") {
		t.Fatalf("duplicate marker accepted: %v", e)
	}
	mustExec(`UPDATE accounts SET deleted_at=NOW(); INSERT INTO accounts(extra) VALUES ('{"upstream_governance_marker":"stable"}'); INSERT INTO accounts(extra) VALUES ('{}'),('{}'),('{"upstream_governance_marker":""}'),('{"upstream_governance_marker":""}')`)
	// Retention is enforced by the write transaction; active previews survive pruning.
	mustExec(`INSERT INTO upstream_governance_snapshots(site_id,site_version,catalog) SELECT $1,2,'{}'::jsonb FROM generate_series(1,25)`, site.ID)
	if e = s.SaveSnapshot(ctx, &Snapshot{SiteID: site.ID, SiteVersion: 2}, nil); e != nil {
		t.Fatal(e)
	}
	var retained int
	if e = fixture.QueryRow(`SELECT COUNT(*) FROM upstream_governance_snapshots WHERE site_id=$1`, site.ID).Scan(&retained); e != nil || retained != 20 {
		t.Fatalf("snapshot retention %d %v", retained, e)
	}
	mustExec(`INSERT INTO upstream_governance_events(site_id,kind) SELECT $1,'test' FROM generate_series(1,1005)`, site.ID)
	if e = s.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "final"}); e != nil {
		t.Fatal(e)
	}
	_, total, e = s.ListEvents(ctx, site.ID, 1, 10)
	if e != nil || total != 1000 {
		t.Fatalf("event retention %d %v", total, e)
	}
	mustExec(`INSERT INTO upstream_governance_checks(site_id,binding_id,model,success,latency_ms) SELECT $1,$2,'text',TRUE,1 FROM generate_series(1,1005)`, site.ID, binding.ID)
	if e = s.AddCheck(ctx, check); e != nil {
		t.Fatal(e)
	}
	_, total, e = s.ListChecks(ctx, site.ID, 1, 10)
	if e != nil || total != 1000 {
		t.Fatalf("check retention %d %v", total, e)
	}
	mustExec(`INSERT INTO upstream_governance_previews(id,site_id,payload,expires_at) SELECT 'old-'||n,$1,'{}'::jsonb,NOW()-INTERVAL '1 hour' FROM generate_series(1,1100) n`, site.ID)
	if e = s.SavePreview(ctx, &Preview{ID: "new-preview", SiteID: site.ID, ExpiresAt: now.Add(time.Minute)}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GetPreview(ctx, site.ID, "preview"); e != nil {
		t.Fatal("unexpired preview pruned", e)
	}
	if e = fixture.QueryRow(`SELECT COUNT(*) FROM upstream_governance_previews WHERE site_id=$1`, site.ID).Scan(&retained); e != nil || retained != 1000 {
		t.Fatalf("preview retention %d %v", retained, e)
	}
	mustExec(`INSERT INTO upstream_governance_previews(id,site_id,payload,expires_at) SELECT 'live-'||n,$1,'{}'::jsonb,NOW()+INTERVAL '1 hour' FROM generate_series(1,998) n`, site.ID)
	if e = s.SavePreview(ctx, &Preview{ID: "over-limit", SiteID: site.ID, ExpiresAt: now.Add(time.Minute)}); !errors.Is(e, ErrConflict) {
		t.Fatalf("unbounded active previews: %v", e)
	}
	mustExec(`DELETE FROM upstream_governance_bindings WHERE site_id=$1`, site.ID)
	if e = s.DeleteSite(ctx, site.ID); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = fixture.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count); e != nil || count != 6 {
		t.Fatalf("governance cleanup touched accounts: %d %v", count, e)
	}
}
