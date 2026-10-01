package upstreamgovernance

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

var _ OperationsStore = (*sqlStore)(nil)

// Only the status projection is read. No session ciphertext, credentials,
// recipient addresses, notification body or SMTP error text leaves SQL.
const workbenchSitesSQL = `SELECT s.id,s.name,s.base_url,s.enabled,s.session_cipher<>'',s.status,s.version,
COALESCE(NULLIF(s.full_interval_seconds,0),s.interval_minutes::bigint*60),s.last_sync_at,snapshot.created_at,COALESCE(snapshot.site_version,0),
s.fast_observe_enabled,COALESCE(NULLIF(s.fast_interval_seconds,0),s.interval_minutes::bigint*60),s.fast_observe_status,s.last_fast_observe_at,s.next_fast_observe_at,
COALESCE((s.balance_monitor->>'enabled')::boolean,false),COALESCE(s.balance_monitor_state->'status'->>'state','unknown'),COALESCE(keys.issue_count,0),
s.fast_observe_source_user_id,s.fast_observe_complete,s.fast_observe_revision,COALESCE(snapshot.catalog->'account'->>'user_id',''),COALESCE(keys.pending_count,0),COALESCE(keys.unknown_count,0),COALESCE(keys.protection_count,0)
FROM upstream_governance_sites s
LEFT JOIN LATERAL (SELECT created_at,site_version,catalog FROM upstream_governance_snapshots WHERE site_id=s.id ORDER BY id DESC LIMIT 1) snapshot ON true
LEFT JOIN (SELECT site_id,
COUNT(*) FILTER(WHERE health_status='confirmed_missing' OR health_status='group_changed' AND missing_count>=2) AS issue_count,
COUNT(*) FILTER(WHERE health_status='suspected_missing' OR health_status='group_changed' AND missing_count<2) AS pending_count,
COUNT(*) FILTER(WHERE health_status NOT IN ('confirmed_missing','suspected_missing','group_changed') AND (health_status<>'present' OR (has_error AND NOT protection_failed))) AS unknown_count,
COUNT(*) FILTER(WHERE health_status='present' AND protection_failed) AS protection_count
FROM (SELECT site_id,COALESCE(key_health->>'status','unknown') AS health_status,COALESCE((key_health->>'missing_count')::int,0) AS missing_count,COALESCE(key_health->>'error_code','')<>'' AS has_error,COALESCE(key_health->>'protection_error','')<>'' AS protection_failed FROM upstream_governance_keys) health GROUP BY site_id) keys ON keys.site_id=s.id
WHERE ($1::bigint=0 OR s.id=$1) ORDER BY s.id`

// Keep every associated site in the result even for a site-filtered request,
// so the shared-group marker and impact count remain truthful.
const workbenchPricingSQL = `SELECT DISTINCT p.local_group_id,g.name,p.protection_reason,p.updated_at,b.site_id
FROM upstream_governance_pricing_policies p JOIN groups g ON g.id=p.local_group_id AND g.deleted_at IS NULL
JOIN upstream_governance_bindings b ON b.local_group_id=g.id OR COALESCE(b.local_group_ids,'[]'::jsonb) @> jsonb_build_array(g.id)
WHERE p.enabled AND p.protected AND ($1::bigint=0 OR EXISTS(SELECT 1 FROM upstream_governance_bindings selected WHERE selected.site_id=$1 AND (selected.local_group_id=g.id OR COALESCE(selected.local_group_ids,'[]'::jsonb) @> jsonb_build_array(g.id)))) ORDER BY p.local_group_id,b.site_id`

const workbenchNotificationsSQL = `SELECT site_id,COUNT(*),CASE WHEN bool_or(status='pending') THEN 'pending' ELSE 'sending' END,MIN(next_attempt_at)
FROM upstream_governance_change_notifications WHERE status IN ('pending','sending') AND last_error<>'' AND ($1::bigint=0 OR site_id=$1) GROUP BY site_id ORDER BY site_id`

func (s *sqlStore) ReadWorkbench(ctx context.Context, q OperationsQuery, now time.Time) (WorkbenchResult, error) {
	if err := validateOperationsQuery(q, false); err != nil {
		return WorkbenchResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return WorkbenchResult{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, workbenchSitesSQL, q.SiteID)
	if err != nil {
		return WorkbenchResult{}, err
	}
	sites := []workbenchSite{}
	for rows.Next() {
		var site workbenchSite
		var snapshotUser string
		if err = rows.Scan(&site.ID, &site.Name, &site.BaseURL, &site.Enabled, &site.HasCredential, &site.Status, &site.Version, &site.FullSeconds, &site.LastAttemptAt, &site.SnapshotAt, &site.SnapshotVersion, &site.FastEnabled, &site.FastSeconds, &site.FastStatus, &site.FastAt, &site.NextFastAt, &site.BalanceEnabled, &site.BalanceState, &site.KeyIssues, &site.FastSourceUserID, &site.FastComplete, &site.FastRevision, &snapshotUser, &site.KeyPending, &site.KeyUnknown, &site.KeyProtectionFailed); err != nil {
			rows.Close()
			return WorkbenchResult{}, err
		}
		site.SnapshotUserID, _ = strconv.ParseInt(snapshotUser, 10, 64)
		sites = append(sites, site)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return WorkbenchResult{}, err
	}
	if q.SiteID > 0 && len(sites) == 0 {
		return WorkbenchResult{}, ErrNotFound
	}
	rows, err = tx.QueryContext(ctx, workbenchPricingSQL, q.SiteID)
	if err != nil {
		return WorkbenchResult{}, err
	}
	pricing := []pricingWorkbenchRow{}
	byGroup := map[int64]int{}
	for rows.Next() {
		var row pricingWorkbenchRow
		var siteID int64
		if err = rows.Scan(&row.GroupID, &row.GroupName, &row.Reason, &row.UpdatedAt, &siteID); err != nil {
			rows.Close()
			return WorkbenchResult{}, err
		}
		idx, ok := byGroup[row.GroupID]
		if !ok {
			idx = len(pricing)
			byGroup[row.GroupID] = idx
			row.Enabled, row.Protected = true, true
			pricing = append(pricing, row)
		}
		pricing[idx].SiteIDs = append(pricing[idx].SiteIDs, siteID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return WorkbenchResult{}, err
	}
	rows, err = tx.QueryContext(ctx, workbenchNotificationsSQL, q.SiteID)
	if err != nil {
		return WorkbenchResult{}, err
	}
	notifications := []notificationWorkbenchRow{}
	for rows.Next() {
		var row notificationWorkbenchRow
		if err = rows.Scan(&row.SiteID, &row.Count, &row.Status, &row.NextAttemptAt); err != nil {
			rows.Close()
			return WorkbenchResult{}, err
		}
		notifications = append(notifications, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return WorkbenchResult{}, err
	}
	result := projectWorkbench(now, sites, pricing, notifications, q)
	if err = tx.Commit(); err != nil {
		return WorkbenchResult{}, err
	}
	return result, nil
}

// A pricing operation is scoped by the site's CURRENT durable local-group
// binding, not attributed to that site's upstream. The shared flag expresses
// current multi-site membership only; no historical causal link is invented.
// Dedup keys are intentionally not selected: group/revision notification keys
// do not uniquely identify pricing operations.
const timelineSQL = `WITH scoped_groups AS (
SELECT DISTINCT g.id,g.name,EXISTS(SELECT 1 FROM upstream_governance_bindings other WHERE other.site_id<>$1 AND (other.local_group_id=g.id OR COALESCE(other.local_group_ids,'[]'::jsonb) @> jsonb_build_array(g.id))) AS shared
FROM groups g JOIN upstream_governance_bindings b ON b.site_id=$1 AND (b.local_group_id=g.id OR COALESCE(b.local_group_ids,'[]'::jsonb) @> jsonb_build_array(g.id))
), records AS (
SELECT e.id::text AS record_id,'event'::text AS kind,e.resource AS resource_id,''::text AS resource_name,'recorded'::text AS status,e.kind AS reason,e.created_at,false AS shared,e.acknowledged,
LEFT(e.before_value,8192) AS before_value,LEFT(e.after_value,8192) AS after_value,NULL::numeric AS before_rate,NULL::numeric AS after_rate,NULL::numeric AS before_cost,NULL::numeric AS after_cost,0::int AS attempts,NULL::timestamptz AS next_attempt_at,NULL::timestamptz AS sent_at,false AS has_error
FROM upstream_governance_events e WHERE e.site_id=$1 AND ($2='all' OR $2='event')
UNION ALL
SELECT p.operation_id,'pricing',p.local_group_id::text,g.name,p.status,p.reason,p.created_at,g.shared,false,'','',p.before_sale,CASE WHEN p.status='applied' THEN p.target_sale END,p.before_cost,p.after_cost,0,NULL::timestamptz,NULL::timestamptz,false
FROM upstream_governance_pricing_operations p JOIN scoped_groups g ON g.id=p.local_group_id WHERE ($2='all' OR $2='pricing')
UNION ALL
SELECT n.id::text,'notification','','',n.status,'',n.created_at,false,false,'','',NULL::numeric,NULL::numeric,NULL::numeric,NULL::numeric,n.attempts,n.next_attempt_at,n.sent_at,n.last_error<>''
FROM upstream_governance_change_notifications n WHERE n.site_id=$1 AND ($2='all' OR $2='notification')
)
`

func (s *sqlStore) ReadTimeline(ctx context.Context, q OperationsQuery, now time.Time) (TimelineResult, error) {
	result := TimelineResult{Items: []TimelineItem{}, Page: q.Page, PageSize: q.PageSize, EvaluatedAt: now}
	if err := validateOperationsQuery(q, true); err != nil {
		return result, err
	}
	if q.Kind == "" {
		q.Kind = "all"
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var site workbenchSite
	if err = tx.QueryRowContext(ctx, `SELECT id,name,base_url FROM upstream_governance_sites WHERE id=$1`, q.SiteID).Scan(&site.ID, &site.Name, &site.BaseURL); err != nil {
		return result, storeError(err)
	}
	if err = tx.QueryRowContext(ctx, timelineSQL+`SELECT COUNT(*) FROM records`, q.SiteID, q.Kind).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, timelineSQL+`SELECT record_id,kind,resource_id,resource_name,status,reason,created_at,shared,acknowledged,before_value,after_value,before_rate,after_rate,before_cost,after_cost,attempts,next_attempt_at,sent_at,has_error FROM records ORDER BY created_at DESC,kind,record_id DESC LIMIT $3 OFFSET $4`, q.SiteID, q.Kind, q.PageSize, (q.Page-1)*q.PageSize)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var record timelineRecord
		if err = rows.Scan(&record.ID, &record.Kind, &record.ResourceID, &record.ResourceName, &record.Status, &record.Reason, &record.CreatedAt, &record.Shared, &record.Acknowledged, &record.Before, &record.After, &record.BeforeRate, &record.AfterRate, &record.BeforeCost, &record.AfterCost, &record.Attempts, &record.NextAttemptAt, &record.SentAt, &record.HasError); err != nil {
			rows.Close()
			return result, err
		}
		result.Items = append(result.Items, projectTimeline(site, record))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
