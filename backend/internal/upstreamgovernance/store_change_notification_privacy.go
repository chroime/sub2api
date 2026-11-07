package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// ChangeNotificationIsPublic checks only the located group and its bindings.
// An unrelated exclusive group at the same site must not mute public notices.
func (s *sqlStore) ChangeNotificationIsPublic(ctx context.Context, item ChangeNotification) (bool, error) {
	if !changeNotificationGroupScoped(item) {
		return true, nil
	}
	if item.GroupResource == "" && item.LocalGroupID <= 0 {
		return false, nil
	}
	if item.GroupResource != "" {
		if public, err := s.changeNotificationRemoteGroupIsPublic(ctx, item.SiteID, item.GroupResource, item.CreatedAt); err != nil || !public {
			return false, err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT target.value::bigint
FROM upstream_governance_bindings b CROSS JOIN LATERAL jsonb_array_elements_text(jsonb_build_array(b.local_group_id) || COALESCE(b.local_group_ids,'[]'::jsonb)) AS target(value)
WHERE b.site_id=$1 AND b.remote_group_id=$2 ORDER BY 1`, item.SiteID, item.GroupResource)
		if err != nil {
			return false, err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return false, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		for _, id := range ids {
			if public, err := s.changeNotificationLocalGroupIsPublic(ctx, id, item.CreatedAt); err != nil || !public {
				return false, err
			}
		}
	}
	if item.LocalGroupID > 0 {
		if public, err := s.changeNotificationLocalGroupIsPublic(ctx, item.LocalGroupID, item.CreatedAt); err != nil || !public {
			return false, err
		}
		return s.changeNotificationLocalGroupRemoteSourcesArePublic(ctx, item.LocalGroupID, item.CreatedAt)
	}
	return true, nil
}

func (s *sqlStore) changeNotificationLocalGroupIsPublic(ctx context.Context, groupID int64, createdAt time.Time) (bool, error) {
	var exclusive sql.NullBool
	var privateHistory bool
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT is_exclusive FROM groups WHERE id=$1 AND deleted_at IS NULL), EXISTS (
SELECT 1 FROM upstream_governance_local_group_changes WHERE group_id=$1 AND created_at >= $2
AND (before_value->'is_exclusive' = 'true'::jsonb OR before_value->'IsExclusive' = 'true'::jsonb
OR after_value->'is_exclusive' = 'true'::jsonb OR after_value->'IsExclusive' = 'true'::jsonb))`, groupID, createdAt).Scan(&exclusive, &privateHistory)
	return err == nil && exclusive.Valid && !exclusive.Bool && !privateHistory, err
}

// Shared with the local-group outbox after it validates its own event snapshots
// and audit sequence. This method deliberately does not recurse through local
// targets, so it cannot widen the scope to unrelated groups at an upstream.
func (s *sqlStore) changeNotificationLocalGroupRemoteSourcesArePublic(ctx context.Context, groupID int64, createdAt time.Time) (bool, error) {
	if public, err := s.ChangeNotificationBindingHistoryIsPublic(ctx, 0, "", groupID, createdAt); err != nil || !public {
		return false, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT site_id,remote_group_id FROM upstream_governance_bindings
WHERE local_group_id=$1 OR COALESCE(local_group_ids,'[]'::jsonb) @> jsonb_build_array($1::bigint)
ORDER BY site_id,remote_group_id`, groupID)
	if err != nil {
		return false, err
	}
	type source struct {
		siteID int64
		group  string
	}
	var sources []source
	for rows.Next() {
		var value source
		if err = rows.Scan(&value.siteID, &value.group); err != nil {
			rows.Close()
			return false, err
		}
		sources = append(sources, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, value := range sources {
		if public, err := s.changeNotificationRemoteGroupIsPublic(ctx, value.siteID, value.group, createdAt); err != nil || !public {
			return false, err
		}
	}
	return true, nil
}

func (s *sqlStore) changeNotificationRemoteGroupIsPublic(ctx context.Context, siteID int64, resource string, createdAt time.Time) (bool, error) {
	var raw []byte
	var privateHistory bool
	err := s.db.QueryRowContext(ctx, `WITH remote_facts AS (
SELECT g.value AS payload,s.created_at AS observed_at FROM upstream_governance_snapshots s
CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(s.catalog->'groups')='array' THEN s.catalog->'groups' ELSE '[]'::jsonb END) g(value)
WHERE s.site_id=$1 AND g.value->>'id'=$2
UNION ALL
SELECT g.value,f.observed_at FROM upstream_governance_fast_observations f
CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(f.groups)='array' THEN f.groups ELSE '[]'::jsonb END) g(value) WHERE f.site_id=$1 AND g.value->>'id'=$2
UNION ALL
SELECT jsonb_build_object('is_exclusive',e.after_value='true'),e.created_at FROM upstream_governance_events e
WHERE e.site_id=$1 AND e.resource=$2 AND e.kind='group_visibility_changed' AND e.after_value IN ('true','false')
)
SELECT (SELECT payload FROM remote_facts ORDER BY observed_at DESC,(payload->'is_exclusive'='true'::jsonb) DESC NULLS LAST LIMIT 1),
EXISTS (SELECT 1 FROM remote_facts WHERE observed_at >= $3 AND payload->'is_exclusive'='true'::jsonb)
OR EXISTS (SELECT 1 FROM upstream_governance_events WHERE site_id=$1 AND resource=$2 AND kind='group_visibility_changed'
AND created_at >= $3 AND (before_value='true' OR after_value='true'))`, siteID, resource, createdAt).Scan(&raw, &privateHistory)
	if err != nil {
		return false, err
	}
	if privateHistory || len(raw) == 0 {
		return false, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false, nil
	}
	if value, exists := fields["is_exclusive"]; exists {
		var exclusive bool
		if string(value) == "null" || json.Unmarshal(value, &exclusive) != nil || exclusive {
			return false, nil
		}
	}
	// New API has no exclusive-group attribute. Older recorded public catalogs
	// also omitted it; a known group with no flag remains compatible.
	return s.ChangeNotificationBindingHistoryIsPublic(ctx, siteID, resource, 0, createdAt)
}

var _ ChangeNotificationPrivacyReader = (*sqlStore)(nil)
