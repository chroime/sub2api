package upstreamgovernance

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// Fast observations replace one catalog row, so retain only the privacy facts
// needed by queued notifications in that same transaction. A removed public
// group's false/false record also preserves evidence for its offline notice.
func recordFastGroupVisibility(ctx context.Context, tx *sql.Tx, siteID int64, at time.Time, before, after []RemoteGroup) error {
	previous, current := make(map[string]RemoteGroup, len(before)), make(map[string]RemoteGroup, len(after))
	for _, group := range before {
		previous[group.ID] = group
	}
	for _, group := range after {
		current[group.ID] = group
	}
	record := func(resource string, wasExclusive, isExclusive bool) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO upstream_governance_events (site_id,kind,resource,before_value,after_value,created_at)
VALUES ($1,'group_visibility_changed',$2,$3,$4,$5)`, siteID, resource, strconv.FormatBool(wasExclusive), strconv.FormatBool(isExclusive), at)
		return err
	}
	for _, group := range after {
		old, exists := previous[group.ID]
		if old.IsExclusive != group.IsExclusive || !exists && group.IsExclusive {
			if err := record(group.ID, old.IsExclusive, group.IsExclusive); err != nil {
				return err
			}
		}
	}
	for _, group := range before {
		if _, exists := current[group.ID]; !exists {
			if err := record(group.ID, group.IsExclusive, group.IsExclusive); err != nil {
				return err
			}
		}
	}
	return nil
}
