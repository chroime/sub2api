package upstreamgovernance

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SiteScheduleStore is optional for legacy/in-memory stores. These deadlines
// are separate from fast observation and all survive a process restart.
type SiteScheduleStore interface {
	NextSiteDueAt(context.Context) (*time.Time, error)
}

const nextSiteDueQuery = `SELECT MIN(deadline) FROM (
 SELECT s.next_sync_at AS deadline FROM upstream_governance_sites s
 WHERE s.enabled AND s.session_cipher<>''
 UNION ALL
 SELECT b.next_probe_at FROM upstream_governance_bindings b
 JOIN upstream_governance_sites s ON s.id=b.site_id
 WHERE s.enabled AND b.probe_enabled AND b.account_id>0 AND b.key_cipher<>'' AND b.probe_model<>''
 UNION ALL
 SELECT (to_jsonb(k)->'key_health'->>'next_check_at')::timestamptz
 FROM upstream_governance_keys k JOIN upstream_governance_sites s ON s.id=k.site_id
 WHERE s.enabled AND s.session_cipher<>'' AND k.key_cipher<>'' AND to_jsonb(k)->'key_health'->>'next_check_at' IS NOT NULL
) AS pending`

func (s *sqlStore) NextSiteDueAt(ctx context.Context) (*time.Time, error) {
	var value sql.NullTime
	err := s.db.QueryRowContext(ctx, nextSiteDueQuery).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !value.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value.Time, nil
}
