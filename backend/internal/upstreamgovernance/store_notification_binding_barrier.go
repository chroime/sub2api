package upstreamgovernance

import (
	"context"
	"time"
)

// ChangeNotificationBindingHistoryIsPublic preserves the privacy cutoff of
// removed associations. The records survive deleting their source site; only
// notices observed at or before the cutoff are invalidated.
func (s *sqlStore) ChangeNotificationBindingHistoryIsPublic(ctx context.Context, siteID int64, resource string, localGroupID int64, createdAt time.Time) (bool, error) {
	var public bool
	err := s.db.QueryRowContext(ctx, `SELECT NOT EXISTS (
SELECT 1 FROM upstream_governance_notification_visibility_barriers
WHERE invalidated_before >= $4 AND (
    (scope='local' AND site_id=0 AND resource='' AND local_group_id=$3 AND $3>0)
    OR (scope='remote' AND site_id=$1 AND resource=$2 AND local_group_id=0 AND $1>0 AND $2<>'')))`, siteID, resource, localGroupID, createdAt).Scan(&public)
	return err == nil && public, err
}
