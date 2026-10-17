package upstreamgovernance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"
)

type sqlStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) Store { return &sqlStore{db: db} }

var _ Store = (*sqlStore)(nil)

// Keep the result column count compatible with pre-261 callers/tests. The
// schedule fields are projected through the existing balance-state slot using
// to_jsonb(table), which returns NULL for columns absent before migration 261.
const siteColumns = `id, name, platform, base_url, proxy_id, enabled, interval_minutes, version, session_cipher, status, last_error, last_sync_at, next_sync_at, created_at, updated_at, balance_monitor, jsonb_build_object('__governance_balance_state', balance_monitor_state, '__fast_interval_seconds', to_jsonb(upstream_governance_sites)->'fast_interval_seconds', '__full_interval_seconds', to_jsonb(upstream_governance_sites)->'full_interval_seconds', '__fast_observe_enabled', to_jsonb(upstream_governance_sites)->'fast_observe_enabled', '__next_fast_observe_at', to_jsonb(upstream_governance_sites)->'next_fast_observe_at', '__last_fast_observe_at', to_jsonb(upstream_governance_sites)->'last_fast_observe_at', '__fast_observe_status', to_jsonb(upstream_governance_sites)->'fast_observe_status', '__fast_observe_error', to_jsonb(upstream_governance_sites)->'fast_observe_error', '__fast_observe_revision', to_jsonb(upstream_governance_sites)->'fast_observe_revision'), login_cipher`

type rowScanner interface{ Scan(...any) error }

func scanSite(row rowScanner) (*Site, error) {
	var s Site
	var monitor, monitorState []byte
	err := row.Scan(&s.ID, &s.Name, &s.Platform, &s.BaseURL, &s.ProxyID, &s.Enabled, &s.IntervalMinutes, &s.Version, &s.SessionCipher, &s.Status, &s.LastError, &s.LastSyncAt, &s.NextSyncAt, &s.CreatedAt, &s.UpdatedAt, &monitor, &monitorState, &s.LoginCipher)
	if err == nil {
		s.BalanceMonitor = defaultBalanceMonitor(s.Platform)
		if err = json.Unmarshal(monitor, &s.BalanceMonitor); err == nil {
			var envelope struct {
				BalanceState json.RawMessage `json:"__governance_balance_state"`
				FastSeconds  *int64          `json:"__fast_interval_seconds"`
				FullSeconds  *int64          `json:"__full_interval_seconds"`
				FastEnabled  *bool           `json:"__fast_observe_enabled"`
				NextFast     *time.Time      `json:"__next_fast_observe_at"`
				LastFast     *time.Time      `json:"__last_fast_observe_at"`
				FastStatus   string          `json:"__fast_observe_status"`
				FastError    string          `json:"__fast_observe_error"`
				FastRevision int64           `json:"__fast_observe_revision"`
			}
			if json.Unmarshal(monitorState, &envelope) == nil && len(envelope.BalanceState) > 0 && string(envelope.BalanceState) != "null" {
				err = json.Unmarshal(envelope.BalanceState, &s.balanceState)
				s.FastIntervalSeconds, s.FullIntervalSeconds = valueOrZero(envelope.FastSeconds), valueOrZero(envelope.FullSeconds)
				if envelope.FastEnabled == nil || *envelope.FastEnabled {
					s.FastObserveEnabled = true
				}
				s.NextFastObserveAt, s.LastFastObserveAt = valueOrTime(envelope.NextFast), valueOrTimePtr(envelope.LastFast)
				s.FastObserveStatus, s.FastObserveError, s.FastObserveRevision = envelope.FastStatus, envelope.FastError, envelope.FastRevision
			} else {
				err = json.Unmarshal(monitorState, &s.balanceState)
				s.FastObserveEnabled = true
			}
		}
		if s.FullIntervalSeconds <= 0 && s.IntervalMinutes > 0 {
			s.FullIntervalSeconds = int64(s.IntervalMinutes) * 60
		}
		if s.FastIntervalSeconds <= 0 {
			s.FastIntervalSeconds = s.FullIntervalSeconds
		}
		if s.NextFastObserveAt.IsZero() {
			s.NextFastObserveAt = s.NextSyncAt
		}
		if s.FastObserveStatus == "" {
			s.FastObserveStatus = "idle"
		}
		if s.BalanceMonitor.Unit == "" {
			s.BalanceMonitor.Unit = balanceUnit(s.Platform)
		}
		if s.BalanceMonitor.Recipients == nil {
			s.BalanceMonitor.Recipients = []string{}
		}
		s.BalanceMonitorStatus = s.balanceState.Status
		if s.BalanceMonitorStatus.State == "" {
			s.BalanceMonitorStatus.State = "disabled"
			if s.BalanceMonitor.Enabled {
				s.BalanceMonitorStatus.State = "unknown"
			}
			s.balanceState.Status = s.BalanceMonitorStatus
		}
	}
	s.HasCredential = s.SessionCipher != ""
	return &s, storeError(err)
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func valueOrTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}

func valueOrTimePtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}
func storeError(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	return e
}
func affected(r sql.Result, e error, missing error) error {
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return missing
	}
	return nil
}
func (s *sqlStore) querySites(ctx context.Context, q string, args ...any) ([]Site, error) {
	rows, e := s.db.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		v, e := scanSite(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func (s *sqlStore) ListSites(ctx context.Context) ([]Site, error) {
	return s.querySites(ctx, `SELECT `+siteColumns+` FROM upstream_governance_sites ORDER BY id LIMIT 1000`)
}
func (s *sqlStore) ListSitesAfter(ctx context.Context, afterID int64, limit int) ([]Site, error) {
	return s.querySites(ctx, `SELECT `+siteColumns+` FROM upstream_governance_sites WHERE id>$1 ORDER BY id LIMIT $2`, afterID, limit)
}
func (s *sqlStore) DueSites(ctx context.Context, now time.Time, limit int) ([]Site, error) {
	if limit < 1 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	return s.querySites(ctx, `SELECT `+siteColumns+` FROM upstream_governance_sites WHERE enabled AND status <> 'local_model' AND ((session_cipher <> '' AND next_sync_at <= $1) OR EXISTS (SELECT 1 FROM upstream_governance_bindings b WHERE b.site_id=upstream_governance_sites.id AND b.probe_enabled AND b.next_probe_at <= $1) OR (session_cipher <> '' AND EXISTS (SELECT 1 FROM upstream_governance_keys k WHERE k.site_id=upstream_governance_sites.id AND k.key_health->>'next_check_at' IS NOT NULL AND (k.key_health->>'next_check_at')::timestamptz <= $1)))
ORDER BY LEAST(
 CASE WHEN session_cipher<>'' THEN next_sync_at ELSE 'infinity'::timestamptz END,
 COALESCE((SELECT MIN(b.next_probe_at) FROM upstream_governance_bindings b WHERE b.site_id=upstream_governance_sites.id AND b.probe_enabled), 'infinity'::timestamptz),
 COALESCE((SELECT MIN((k.key_health->>'next_check_at')::timestamptz) FROM upstream_governance_keys k WHERE k.site_id=upstream_governance_sites.id AND k.key_health->>'next_check_at' IS NOT NULL), 'infinity'::timestamptz)
), id LIMIT $2`, now, limit)
}

// DueFastObservations is intentionally separate from DueSites: a fast group
// read must not be delayed by full catalog, key, probe, or model work.
func (s *sqlStore) DueFastObservations(ctx context.Context, now time.Time, limit int) ([]Site, error) {
	if limit < 1 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	return s.querySites(ctx, `SELECT `+siteColumns+` FROM upstream_governance_sites WHERE enabled AND status <> 'local_model' AND fast_observe_enabled AND session_cipher<>'' AND next_fast_observe_at <= $1 AND (fast_observe_status <> 'running' OR fast_observe_reserved_at IS NULL OR fast_observe_reserved_at < $1 - INTERVAL '5 minutes') ORDER BY next_fast_observe_at,id LIMIT $2`, now, limit)
}

func (s *sqlStore) NextFastObservationAt(ctx context.Context) (*time.Time, error) {
	var value *time.Time
	err := s.db.QueryRowContext(ctx, `SELECT MIN(next_fast_observe_at) FROM upstream_governance_sites WHERE enabled AND fast_observe_enabled AND session_cipher<>'' AND (fast_observe_status <> 'running' OR fast_observe_reserved_at IS NULL OR fast_observe_reserved_at < NOW() - INTERVAL '5 minutes')`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) || value == nil {
		return nil, nil
	}
	if err != nil {
		if isMissingSecondsSchema(err) {
			return nil, ErrUnsupported
		}
		return nil, err
	}
	return value, nil
}

func (s *sqlStore) ReserveFastObservation(ctx context.Context, siteID int64, dueAt, reservedAt time.Time) (bool, error) {
	if dueAt.IsZero() {
		dueAt = reservedAt
	}
	if reservedAt.IsZero() {
		reservedAt = time.Now().UTC()
	}
	r, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_sites SET fast_observe_status='running',fast_observe_error='',fast_observe_reserved_at=$3::timestamptz,updated_at=NOW() WHERE id=$1 AND enabled AND fast_observe_enabled AND next_fast_observe_at <= $2::timestamptz AND (fast_observe_status <> 'running' OR fast_observe_reserved_at IS NULL OR fast_observe_reserved_at < $3::timestamptz - INTERVAL '5 minutes')`, siteID, dueAt, reservedAt)
	if isMissingSecondsSchema(err) {
		return false, ErrUnsupported
	}
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

func (s *sqlStore) ObserveFastResult(ctx context.Context, siteID int64, observedAt, nextAt time.Time, status, message string, observation GroupObservation) (FastObservationResult, error) {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	if nextAt.IsZero() {
		nextAt = observedAt.Add(time.Minute)
	}
	if status == "" {
		status = "healthy"
	}
	if !observation.GroupsComplete {
		_, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_sites SET fast_observe_status=$2,fast_observe_error=$3,next_fast_observe_at=$4,fast_observe_reserved_at=NULL,updated_at=NOW() WHERE id=$1`, siteID, status, message, nextAt)
		return FastObservationResult{ObservedAt: observedAt}, err
	}
	fingerprint, err := groupObservationFingerprint(observation)
	if err != nil {
		return FastObservationResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FastObservationResult{}, err
	}
	defer tx.Rollback()
	var previousFingerprint string
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,revision FROM upstream_governance_fast_observations WHERE site_id=$1 FOR UPDATE`, siteID).Scan(&previousFingerprint, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		previousFingerprint = ""
		revision = 0
	} else if err != nil {
		return FastObservationResult{}, err
	}
	changed := previousFingerprint != fingerprint
	if changed {
		revision++
	}
	raw, err := json.Marshal(observation.Groups)
	if err != nil {
		return FastObservationResult{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_fast_observations(site_id,revision,fingerprint,source_user_id,groups_complete,groups,observed_at,updated_at) VALUES($1,$2,$3,$4,true,$5::jsonb,$6,NOW()) ON CONFLICT(site_id) DO UPDATE SET revision=EXCLUDED.revision,fingerprint=EXCLUDED.fingerprint,source_user_id=EXCLUDED.source_user_id,groups_complete=EXCLUDED.groups_complete,groups=EXCLUDED.groups,observed_at=EXCLUDED.observed_at,updated_at=NOW()`, siteID, revision, fingerprint, observation.SourceUserID, string(raw), observedAt)
	if err != nil {
		return FastObservationResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_governance_sites SET fast_observe_status=$2,fast_observe_error=$3,last_fast_observe_at=$4,next_fast_observe_at=$5,fast_observe_reserved_at=NULL,fast_observe_source_user_id=$6,fast_observe_complete=true,fast_observe_revision=$7,updated_at=NOW() WHERE id=$1`, siteID, status, message, observedAt, nextAt, observation.SourceUserID, revision)
	if err != nil {
		return FastObservationResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return FastObservationResult{}, err
	}
	return FastObservationResult{Changed: changed, Revision: revision, ObservedAt: observedAt}, nil
}

func (s *sqlStore) LatestCatalogRevision(ctx context.Context, siteID int64) (*CatalogObservation, error) {
	var out CatalogObservation
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT site_id,revision,fingerprint,source_user_id,groups_complete,groups,observed_at FROM upstream_governance_fast_observations WHERE site_id=$1`, siteID).Scan(&out.SiteID, &out.Revision, &out.Fingerprint, &out.SourceUserID, &out.GroupsComplete, &raw, &out.ObservedAt)
	if err != nil {
		return nil, storeError(err)
	}
	if err = json.Unmarshal(raw, &out.Groups); err != nil {
		return nil, err
	}
	return &out, nil
}
func (s *sqlStore) CreateSite(ctx context.Context, v *Site) error {
	fast, full, next := normalizedSchedule(*v)
	err := s.db.QueryRowContext(ctx, `INSERT INTO upstream_governance_sites (name, platform, base_url, proxy_id, enabled, interval_minutes, session_cipher, status, last_error, next_sync_at, login_cipher, fast_interval_seconds, full_interval_seconds, next_fast_observe_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id,version,created_at,updated_at`, v.Name, v.Platform, v.BaseURL, v.ProxyID, v.Enabled, v.IntervalMinutes, v.SessionCipher, v.Status, v.LastError, v.NextSyncAt, v.LoginCipher, fast, full, next).Scan(&v.ID, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if isMissingSecondsSchema(err) {
		err = s.db.QueryRowContext(ctx, `INSERT INTO upstream_governance_sites (name, platform, base_url, proxy_id, enabled, interval_minutes, session_cipher, status, last_error, next_sync_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,version,created_at,updated_at`, v.Name, v.Platform, v.BaseURL, v.ProxyID, v.Enabled, v.IntervalMinutes, v.SessionCipher, v.Status, v.LastError, v.NextSyncAt).Scan(&v.ID, &v.Version, &v.CreatedAt, &v.UpdatedAt)
		if err == nil && v.LoginCipher != "" {
			if _, updateErr := s.db.ExecContext(ctx, `UPDATE upstream_governance_sites SET login_cipher=$1 WHERE id=$2`, v.LoginCipher, v.ID); updateErr != nil && !isMissingSecondsSchema(updateErr) {
				err = updateErr
			}
		}
	}
	if err == nil {
		v.FastIntervalSeconds, v.FullIntervalSeconds, v.NextFastObserveAt = fast, full, next
	}
	return err
}
func (s *sqlStore) GetSite(ctx context.Context, id int64) (*Site, error) {
	return scanSite(s.db.QueryRowContext(ctx, `SELECT `+siteColumns+` FROM upstream_governance_sites WHERE id=$1`, id))
}
func (s *sqlStore) UpdateSite(ctx context.Context, v *Site, version int64) error {
	monitor, e := json.Marshal(v.BalanceMonitor)
	if e != nil {
		return e
	}
	monitorState, e := json.Marshal(v.balanceState)
	if e != nil {
		return e
	}
	fast, full, nextFast := normalizedSchedule(*v)
	var next int64
	var updated time.Time
	e = s.db.QueryRowContext(ctx, `UPDATE upstream_governance_sites SET name=$2,platform=$3,base_url=$4,proxy_id=$5,enabled=$6,interval_minutes=$7,session_cipher=$8,status=$9,last_error=$10,next_sync_at=$11,version=version+1,updated_at=NOW(),balance_monitor=$13::jsonb,balance_monitor_state=$14::jsonb,login_cipher=$15,fast_interval_seconds=$16,full_interval_seconds=$17,next_fast_observe_at=$18,last_sync_at=CASE WHEN base_url<>$4 OR platform<>$3 THEN NULL ELSE last_sync_at END WHERE id=$1 AND version=$12 RETURNING version,updated_at`, v.ID, v.Name, v.Platform, v.BaseURL, v.ProxyID, v.Enabled, v.IntervalMinutes, v.SessionCipher, v.Status, v.LastError, v.NextSyncAt, version, string(monitor), string(monitorState), v.LoginCipher, fast, full, nextFast).Scan(&next, &updated)
	if isMissingSecondsSchema(e) {
		e = s.db.QueryRowContext(ctx, `UPDATE upstream_governance_sites SET name=$2,platform=$3,base_url=$4,proxy_id=$5,enabled=$6,interval_minutes=$7,session_cipher=$8,status=$9,last_error=$10,next_sync_at=$11,version=version+1,updated_at=NOW(),balance_monitor=$13::jsonb,balance_monitor_state=$14::jsonb,login_cipher=$15,last_sync_at=CASE WHEN base_url<>$4 OR platform<>$3 THEN NULL ELSE last_sync_at END WHERE id=$1 AND version=$12 RETURNING version,updated_at`, v.ID, v.Name, v.Platform, v.BaseURL, v.ProxyID, v.Enabled, v.IntervalMinutes, v.SessionCipher, v.Status, v.LastError, v.NextSyncAt, version, string(monitor), string(monitorState), v.LoginCipher).Scan(&next, &updated)
	}
	if errors.Is(e, sql.ErrNoRows) {
		return ErrConflict
	}
	if e == nil {
		v.Version = next
		v.UpdatedAt = updated
		v.FastIntervalSeconds, v.FullIntervalSeconds, v.NextFastObserveAt = fast, full, nextFast
		if _, toggleErr := s.db.ExecContext(ctx, `UPDATE upstream_governance_sites SET fast_observe_enabled=$1 WHERE id=$2`, v.FastObserveEnabled, v.ID); toggleErr != nil && !isMissingSecondsSchema(toggleErr) {
			return toggleErr
		}
		v.HasCredential = v.SessionCipher != ""
	}
	return e
}

func normalizedSchedule(v Site) (fast, full int64, next time.Time) {
	full = v.FullIntervalSeconds
	if full <= 0 && v.IntervalMinutes > 0 {
		full = int64(v.IntervalMinutes) * 60
	}
	if full <= 0 {
		full = 900
	}
	fast = v.FastIntervalSeconds
	if fast <= 0 {
		fast = full
	}
	next = v.NextFastObserveAt
	if next.IsZero() {
		next = v.NextSyncAt
	}
	if next.IsZero() {
		next = time.Now().UTC()
	}
	return
}

func isMissingSecondsSchema(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "fast_interval_seconds") || strings.Contains(message, "next_fast_observe_at") || strings.Contains(message, "column") && strings.Contains(message, "does not exist")
}

func (s *sqlStore) StageLoginChallenge(ctx context.Context, siteID, version int64, encrypted string) error {
	r, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_sites SET login_cipher=$3 WHERE id=$1 AND version=$2`, siteID, version, encrypted)
	return affected(r, err, ErrConflict)
}
func (s *sqlStore) DeleteSite(ctx context.Context, id int64) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// The service holds the advisory site lock; this row lock also prevents new
	// FK references from appearing between the dependency check and deletion.
	var siteID int64
	if e = tx.QueryRowContext(ctx, `SELECT id FROM upstream_governance_sites WHERE id=$1 FOR UPDATE`, id).Scan(&siteID); e != nil {
		return storeError(e)
	}
	var inUse bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM upstream_governance_bindings b WHERE b.site_id=$1
AND (b.account_id=0 OR EXISTS (SELECT 1 FROM accounts a WHERE a.id=b.account_id AND a.deleted_at IS NULL))
) OR EXISTS (SELECT 1 FROM upstream_governance_keys WHERE site_id=$1)`, id).Scan(&inUse); e != nil {
		return e
	}
	if inUse {
		return ErrSiteInUse
	}
	// Retired local accounts leave historical bindings. Remove only those
	// orphans; pending imports and managed keys retain their ownership records.
	if _, e = tx.ExecContext(ctx, `DELETE FROM upstream_governance_bindings b WHERE b.site_id=$1 AND b.account_id>0 AND NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=b.account_id AND a.deleted_at IS NULL)`, id); e != nil {
		return e
	}
	r, e := tx.ExecContext(ctx, `DELETE FROM upstream_governance_sites WHERE id=$1`, id)
	if e = affected(r, e, ErrNotFound); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *sqlStore) ObserveSite(ctx context.Context, id int64, status, message string, last, next time.Time) error {
	var success any
	if !last.IsZero() {
		success = last
	}
	r, e := s.db.ExecContext(ctx, `UPDATE upstream_governance_sites SET status=$2,last_error=$3,last_sync_at=COALESCE($4,last_sync_at),next_sync_at=$5,updated_at=NOW() WHERE id=$1`, id, status, message, success, next)
	return affected(r, e, ErrNotFound)
}

// A namespaced 64-bit lock supports the full BIGINT site ID range. Its dedicated
// session is never returned to the pool while a possibly-held lock remains.
func (s *sqlStore) LockSite(ctx context.Context, id int64) (func(), bool, error) {
	conn, e := s.db.Conn(ctx)
	if e != nil {
		return nil, false, e
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "sub2api:upstream-governance:site:%d", id)
	key := int64(h.Sum64())
	discard := func() { _ = conn.Raw(func(any) error { return driver.ErrBadConn }); _ = conn.Close() }
	var locked bool
	if e = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&locked); e != nil {
		discard()
		return nil, false, e
	}
	if !locked {
		_ = conn.Close()
		return nil, false, nil
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var unlocked bool
			if e := conn.QueryRowContext(releaseCtx, `SELECT pg_advisory_unlock($1)`, key).Scan(&unlocked); e != nil || !unlocked {
				discard()
				return
			}
			_ = conn.Close()
		})
	}, true, nil
}
func (s *sqlStore) LatestSnapshot(ctx context.Context, id int64) (*Snapshot, error) {
	var v Snapshot
	var data []byte
	e := s.db.QueryRowContext(ctx, `SELECT id,site_id,site_version,catalog,created_at FROM upstream_governance_snapshots WHERE site_id=$1 ORDER BY id DESC LIMIT 1`, id).Scan(&v.ID, &v.SiteID, &v.SiteVersion, &data, &v.CreatedAt)
	if e != nil {
		return nil, storeError(e)
	}
	if e = json.Unmarshal(data, &v.Catalog); e != nil {
		return nil, e
	}
	return &v, nil
}

func (s *sqlStore) EventGroupName(ctx context.Context, siteID int64, resource string, at time.Time) (string, error) {
	lookup := func(query string, args ...any) (string, error) {
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return "", err
			}
			var catalog Catalog
			if err := json.Unmarshal(raw, &catalog); err != nil {
				continue
			}
			for _, group := range catalog.Groups {
				if group.ID == resource && group.Name != "" {
					return group.Name, nil
				}
			}
		}
		return "", rows.Err()
	}

	name, err := lookup(`SELECT catalog FROM upstream_governance_snapshots WHERE site_id=$1 AND created_at <= $2 ORDER BY id DESC LIMIT 20`, siteID, at)
	if err != nil || name != "" {
		return name, err
	}
	// Snapshots are retained as a rolling window. If the event predates that
	// window, use the current catalog rather than exposing a bare group ID.
	return lookup(`SELECT catalog FROM upstream_governance_snapshots WHERE site_id=$1 ORDER BY id DESC LIMIT 1`, siteID)
}
func (s *sqlStore) SaveSnapshot(ctx context.Context, v *Snapshot, events []Event) error {
	data, e := json.Marshal(v.Catalog)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id int64
	var created time.Time
	if e = tx.QueryRowContext(ctx, `INSERT INTO upstream_governance_snapshots (site_id,site_version,catalog) VALUES ($1,$2,$3::jsonb) RETURNING id,created_at`, v.SiteID, v.SiteVersion, string(data)).Scan(&id, &created); e != nil {
		return e
	}
	for _, event := range events {
		if _, e = tx.ExecContext(ctx, `INSERT INTO upstream_governance_events (site_id,kind,resource,before_value,after_value) VALUES ($1,$2,$3,$4,$5)`, v.SiteID, event.Kind, event.Resource, event.Before, event.After); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM upstream_governance_snapshots WHERE site_id=$1 AND id NOT IN (SELECT id FROM upstream_governance_snapshots WHERE site_id=$1 ORDER BY id DESC LIMIT 20)`, v.SiteID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, pruneEvents, v.SiteID); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		v.ID = id
		v.CreatedAt = created
	}
	return e
}

const pruneEvents = `DELETE FROM upstream_governance_events WHERE site_id=$1 AND id NOT IN (SELECT id FROM upstream_governance_events WHERE site_id=$1 ORDER BY id DESC LIMIT 1000)`

func pageBounds(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	if page > 10001 {
		page = 10001
	}
	return size, (page - 1) * size
}
func (s *sqlStore) ListEvents(ctx context.Context, site int64, page, size int) ([]Event, int64, error) {
	limit, offset := pageBounds(page, size)
	var total int64
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_governance_events WHERE site_id=$1`, site).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id,site_id,kind,resource,before_value,after_value,acknowledged,created_at FROM upstream_governance_events WHERE site_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, site, limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.ID, &v.SiteID, &v.Kind, &v.Resource, &v.Before, &v.After, &v.Acknowledged, &v.CreatedAt); e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
func (s *sqlStore) AddEvent(ctx context.Context, v *Event) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id int64
	var created time.Time
	if e = tx.QueryRowContext(ctx, `INSERT INTO upstream_governance_events (site_id,kind,resource,before_value,after_value) VALUES ($1,$2,$3,$4,$5) RETURNING id,created_at`, v.SiteID, v.Kind, v.Resource, v.Before, v.After).Scan(&id, &created); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, pruneEvents, v.SiteID); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		v.ID = id
		v.CreatedAt = created
	}
	return e
}
func (s *sqlStore) AckEvent(ctx context.Context, site, id int64) error {
	r, e := s.db.ExecContext(ctx, `UPDATE upstream_governance_events SET acknowledged=TRUE WHERE site_id=$1 AND id=$2`, site, id)
	return affected(r, e, ErrNotFound)
}
func (s *sqlStore) SavePreview(ctx context.Context, v *Preview) error {
	frozen := *v
	frozen.Result = nil
	data, e := json.Marshal(frozen)
	if e != nil {
		return e
	}
	r, e := s.db.ExecContext(ctx, `INSERT INTO upstream_governance_previews (id,site_id,payload,expires_at) SELECT $1,$2,$3::jsonb,$4 WHERE (SELECT COUNT(*) FROM upstream_governance_previews WHERE site_id=$2 AND expires_at > NOW()) < 1000 ON CONFLICT (id) DO NOTHING`, v.ID, v.SiteID, string(data), v.ExpiresAt)
	if e = affected(r, e, ErrConflict); e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, `DELETE FROM upstream_governance_previews WHERE site_id=$1 AND expires_at < NOW() AND id NOT IN (SELECT id FROM upstream_governance_previews WHERE site_id=$1 ORDER BY expires_at DESC LIMIT 1000)`, v.SiteID)
	return e
}
func (s *sqlStore) GetPreview(ctx context.Context, site int64, id string) (*Preview, error) {
	var payload, result []byte
	e := s.db.QueryRowContext(ctx, `SELECT payload, result FROM upstream_governance_previews WHERE site_id=$1 AND id=$2`, site, id).Scan(&payload, &result)
	if e != nil {
		return nil, storeError(e)
	}
	var v Preview
	if e = json.Unmarshal(payload, &v); e != nil {
		return nil, e
	}
	if len(result) > 0 {
		if e = json.Unmarshal(result, &v.Result); e != nil {
			return nil, e
		}
	}
	return &v, nil
}
func (s *sqlStore) SavePreviewResult(ctx context.Context, site int64, id string, v *ApplyResult) error {
	data, e := json.Marshal(v)
	if e != nil {
		return e
	}
	r, e := s.db.ExecContext(ctx, `UPDATE upstream_governance_previews SET result=$3::jsonb WHERE site_id=$1 AND id=$2`, site, id, string(data))
	return affected(r, e, ErrNotFound)
}
func (s *sqlStore) ListBindings(ctx context.Context, site int64) ([]Binding, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,site_id,remote_group_id,platform,local_group_id,account_id,marker,key_cipher,probe_enabled,probe_model,probe_interval_minutes,next_probe_at,COALESCE(local_group_ids,jsonb_build_array(local_group_id)) FROM upstream_governance_bindings WHERE site_id=$1 ORDER BY id LIMIT 1000`, site)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		var v Binding
		var groupIDs []byte
		if e = rows.Scan(&v.ID, &v.SiteID, &v.RemoteGroupID, &v.Platform, &v.LocalGroupID, &v.AccountID, &v.Marker, &v.KeyCipher, &v.ProbeEnabled, &v.ProbeModel, &v.ProbeIntervalMinutes, &v.NextProbeAt, &groupIDs); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(groupIDs, &v.LocalGroupIDs); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *sqlStore) SaveBinding(ctx context.Context, v *Binding) error {
	ids, e := NormalizeLocalGroupIDs(v.LocalGroupIDs, v.LocalGroupID)
	if e != nil {
		return e
	}
	v.LocalGroupIDs, v.LocalGroupID = ids, ids[0]
	raw, _ := json.Marshal(ids)
	e = s.db.QueryRowContext(ctx, `INSERT INTO upstream_governance_bindings (site_id,remote_group_id,platform,local_group_id,account_id,marker,key_cipher,probe_enabled,probe_model,probe_interval_minutes,next_probe_at,local_group_ids) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb) ON CONFLICT (site_id, remote_group_id, platform) DO UPDATE SET local_group_id=EXCLUDED.local_group_id,local_group_ids=EXCLUDED.local_group_ids,account_id=EXCLUDED.account_id,key_cipher=EXCLUDED.key_cipher,probe_enabled=EXCLUDED.probe_enabled,probe_model=EXCLUDED.probe_model,probe_interval_minutes=EXCLUDED.probe_interval_minutes,next_probe_at=EXCLUDED.next_probe_at WHERE upstream_governance_bindings.marker=EXCLUDED.marker RETURNING id`, v.SiteID, v.RemoteGroupID, v.Platform, v.LocalGroupID, v.AccountID, v.Marker, v.KeyCipher, v.ProbeEnabled, v.ProbeModel, v.ProbeIntervalMinutes, v.NextProbeAt, string(raw)).Scan(&v.ID)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrConflict
	}
	return e
}
func (s *sqlStore) AddCheck(ctx context.Context, v *Check) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id int64
	var created time.Time
	if e = tx.QueryRowContext(ctx, `INSERT INTO upstream_governance_checks (site_id,binding_id,model,success,latency_ms,error_code) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id,created_at`, v.SiteID, v.BindingID, v.Model, v.Success, v.LatencyMS, v.ErrorCode).Scan(&id, &created); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM upstream_governance_checks WHERE site_id=$1 AND id NOT IN (SELECT id FROM upstream_governance_checks WHERE site_id=$1 ORDER BY id DESC LIMIT 1000)`, v.SiteID); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		v.ID = id
		v.CreatedAt = created
	}
	return e
}
func (s *sqlStore) LatestCheck(ctx context.Context, site, binding int64) (*Check, error) {
	var v Check
	e := s.db.QueryRowContext(ctx, `SELECT id,site_id,binding_id,model,success,latency_ms,error_code,created_at FROM upstream_governance_checks WHERE site_id=$1 AND binding_id=$2 ORDER BY id DESC LIMIT 1`, site, binding).Scan(&v.ID, &v.SiteID, &v.BindingID, &v.Model, &v.Success, &v.LatencyMS, &v.ErrorCode, &v.CreatedAt)
	if e != nil {
		return nil, storeError(e)
	}
	return &v, nil
}
func (s *sqlStore) ListChecks(ctx context.Context, site int64, page, size int) ([]Check, int64, error) {
	limit, offset := pageBounds(page, size)
	var total int64
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_governance_checks WHERE site_id=$1`, site).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id,site_id,binding_id,model,success,latency_ms,error_code,created_at FROM upstream_governance_checks WHERE site_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, site, limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []Check{}
	for rows.Next() {
		var v Check
		if e = rows.Scan(&v.ID, &v.SiteID, &v.BindingID, &v.Model, &v.Success, &v.LatencyMS, &v.ErrorCode, &v.CreatedAt); e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
