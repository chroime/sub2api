package upstreamgovernance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"time"
)

func storeFixture(t *testing.T) (Store, sqlmock.Sqlmock) {
	t.Helper()
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		if e := m.ExpectationsWereMet(); e != nil {
			t.Error(e)
		}
	})
	return NewSQLStore(db), m
}
func TestSQLStoreOptimisticConflict(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`UPDATE upstream_governance_sites`).WillReturnError(sql.ErrNoRows)
	site := &Site{ID: 1, Version: 4}
	if e := s.UpdateSite(context.Background(), site, 3); !errors.Is(e, ErrConflict) {
		t.Fatalf("want conflict got %v", e)
	}
	if site.Version != 4 {
		t.Fatal("failed write mutated version")
	}
}
func TestSQLStoreSnapshotEventFailureRollsBack(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectBegin()
	m.ExpectQuery(`INSERT INTO upstream_governance_snapshots`).WithArgs(int64(1), int64(0), sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(9, time.Now()))
	m.ExpectExec(`INSERT INTO upstream_governance_events`).WillReturnError(errors.New("event write failed"))
	m.ExpectRollback()
	if e := s.SaveSnapshot(context.Background(), &Snapshot{SiteID: 1}, []Event{{Kind: "added"}}); e == nil {
		t.Fatal("want rollback error")
	}
}
func TestSQLStoreLockContention(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`SELECT pg_try_advisory_lock`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(false))
	release, ok, e := s.LockSite(context.Background(), 3)
	if e != nil || ok || release != nil {
		t.Fatalf("release=%v ok=%v err=%v", release != nil, ok, e)
	}
}
func TestSQLStoreLockReleaseAfterCancellation(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`SELECT pg_try_advisory_lock`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	m.ExpectQuery(`SELECT pg_advisory_unlock`).WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(true))
	ctx, cancel := context.WithCancel(context.Background())
	release, ok, e := s.LockSite(ctx, 3)
	if e != nil || !ok {
		t.Fatal(e)
	}
	cancel()
	release()
	release()
}
func TestSQLStoreImmutablePreview(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectExec(`INSERT INTO upstream_governance_previews`).WillReturnResult(sqlmock.NewResult(0, 0))
	if e := s.SavePreview(context.Background(), &Preview{ID: "existing", SiteID: 1}); !errors.Is(e, ErrConflict) {
		t.Fatalf("want conflict got %v", e)
	}
}
func TestSQLStorePreviewReplay(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`SELECT payload, result FROM upstream_governance_previews`).WithArgs(int64(1), "preview").WillReturnRows(sqlmock.NewRows([]string{"payload", "result"}).AddRow(`{"id":"preview","site_id":1,"rows":[{"marker":"stable"}]}`, `{"preview_id":"preview","items":[{"status":"success","account_id":7}]}`))
	p, e := s.GetPreview(context.Background(), 1, "preview")
	if e != nil {
		t.Fatal(e)
	}
	if p.Rows[0].Marker != "stable" || p.Result.Items[0].AccountID != 7 {
		t.Fatalf("bad preview %#v", p)
	}
}
func TestSQLStoreBindingPreservesSecretAndUniqueTuple(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`INSERT INTO upstream_governance_bindings[\s\S]*ON CONFLICT \(site_id, remote_group_id, platform\) DO UPDATE`).WithArgs(int64(2), "remote", "openai", int64(3), int64(0), "marker", "encrypted-key", false, "", 30, sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(4))
	b := &Binding{SiteID: 2, RemoteGroupID: "remote", Platform: "openai", LocalGroupID: 3, Marker: "marker", KeyCipher: "encrypted-key", ProbeIntervalMinutes: 30}
	if e := s.SaveBinding(context.Background(), b); e != nil {
		t.Fatal(e)
	}
	if b.ID != 4 {
		t.Fatal(b.ID)
	}
}
func TestSQLStoreUnlockFailureDiscardsConnection(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			s := NewSQLStore(db)
			m.ExpectQuery(`SELECT pg_try_advisory_lock`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
			q := m.ExpectQuery(`SELECT pg_advisory_unlock`)
			if failed {
				q.WillReturnError(errors.New("connection lost"))
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(false))
			}
			release, ok, e := s.LockSite(context.Background(), 7)
			if e != nil || !ok {
				t.Fatal(e)
			}
			release()
			if db.Stats().Idle != 0 {
				t.Fatal("possibly locked connection returned to pool")
			}
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestSQLStoreSnapshotReturnsCompleteCatalog(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`SELECT id,site_id,site_version,catalog,created_at`).WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "site_id", "site_version", "catalog", "created_at"}).AddRow(9, 2, 4, `{"groups":[{"id":"g","rate_multiplier":2,"user_rate_multiplier":0.8,"resolved_rate_multiplier":0.8,"peak_rate_enabled":true,"peak_start":"10:00","prices":[{"model":"m","input":null,"output":1,"unit":"USD/M"}]}],"channels":[{"name":"channel"}],"warnings":["unknown input price"]}`, time.Now()))
	v, e := s.LatestSnapshot(context.Background(), 2)
	if e != nil {
		t.Fatal(e)
	}
	g := v.Catalog.Groups[0]
	if v.SiteVersion != 4 || *g.ResolvedRateMultiplier != 0.8 || g.Prices[0].Input != nil || g.PeakStart != "10:00" || len(v.Catalog.Channels) != 1 || len(v.Catalog.Warnings) != 1 {
		t.Fatalf("lost data: %#v", v)
	}
}
func TestSQLStoreBindingMarkerConflict(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`INSERT INTO upstream_governance_bindings`).WillReturnError(sql.ErrNoRows)
	if e := s.SaveBinding(context.Background(), &Binding{}); !errors.Is(e, ErrConflict) {
		t.Fatalf("want conflict got %v", e)
	}
}
func TestSQLStorePaginationBounded(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`SELECT COUNT`).WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectQuery(`SELECT id,site_id,kind`).WithArgs(int64(2), 100, 1000000).WillReturnRows(sqlmock.NewRows([]string{"id", "site_id", "kind", "resource", "before_value", "after_value", "acknowledged", "created_at"}))
	v, total, e := s.ListEvents(context.Background(), 2, int(^uint(0)>>1), int(^uint(0)>>1))
	if e != nil || total != 0 || len(v) != 0 {
		t.Fatalf("%v %d %v", v, total, e)
	}
}

func TestSQLStoreDueSitesIncludesScheduledChecks(t *testing.T) {
	s, m := storeFixture(t)
	now := time.Now()
	m.ExpectQuery(`SELECT [\s\S]+ FROM upstream_governance_sites[\s\S]+session_cipher <> ''[\s\S]+next_sync_at <= \$1[\s\S]+OR EXISTS[\s\S]+upstream_governance_bindings[\s\S]+probe_enabled[\s\S]+next_probe_at <= \$1`).WithArgs(now, 20).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "base_url", "proxy_id", "enabled", "interval_minutes", "version", "session_cipher", "status", "last_error", "last_sync_at", "next_sync_at", "created_at", "updated_at"}))
	if _, e := s.DueSites(context.Background(), now, 1000); e != nil {
		t.Fatal(e)
	}
}

func TestSQLStoreLatestCheckIsScopedToSiteAndBinding(t *testing.T) {
	s, m := storeFixture(t)
	now := time.Now()
	m.ExpectQuery(`SELECT id,site_id,binding_id,model,success,latency_ms,error_code,created_at FROM upstream_governance_checks WHERE site_id=\$1 AND binding_id=\$2 ORDER BY id DESC LIMIT 1`).WithArgs(int64(2), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "site_id", "binding_id", "model", "success", "latency_ms", "error_code", "created_at"}).AddRow(31, 2, 7, "fixture-model", false, 23, "rate_limited", now))
	v, e := s.LatestCheck(context.Background(), 2, 7)
	if e != nil || v == nil || v.ID != 31 || v.SiteID != 2 || v.BindingID != 7 || v.Model != "fixture-model" || v.Success || v.LatencyMS != 23 || v.ErrorCode != "rate_limited" || !v.CreatedAt.Equal(now) {
		t.Fatalf("latest check %#v %v", v, e)
	}
}

func TestSQLStoreLatestCheckMissing(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectQuery(`SELECT id,site_id,binding_id,model,success,latency_ms,error_code,created_at`).WithArgs(int64(2), int64(7)).WillReturnError(sql.ErrNoRows)
	v, e := s.LatestCheck(context.Background(), 2, 7)
	if v != nil || !errors.Is(e, ErrNotFound) {
		t.Fatalf("missing latest check %#v %v", v, e)
	}
}
