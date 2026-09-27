package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccountgroup "github.com/Wei-Shaw/sub2api/ent/accountgroup"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"slices"
)

// Check on the same transaction/row lock used by the normal account write.
// Reading groups separately after acquiring the parent lock observes the latest
// committed rebind, including a writer that was already in flight.
func checkGovernanceAccountCAS(ctx context.Context, q sqlQueryer, id int64, expected string) error {
	a := &service.Account{ID: id}
	var credentials, extra []byte
	var proxy, parent sql.NullInt64
	var notes sql.NullString
	var rate sql.NullFloat64
	err := scanSingleRow(ctx, q, `SELECT name,platform,type,status,credentials,extra,proxy_id,rate_multiplier,parent_account_id,notes,concurrency,priority FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, []any{id}, &a.Name, &a.Platform, &a.Type, &a.Status, &credentials, &extra, &proxy, &rate, &parent, &notes, &a.Concurrency, &a.Priority)
	if errors.Is(err, sql.ErrNoRows) {
		return gov.ErrConflict
	}
	if err != nil {
		return err
	}
	if json.Unmarshal(credentials, &a.Credentials) != nil || json.Unmarshal(extra, &a.Extra) != nil {
		return gov.ErrConflict
	}
	if parent.Valid {
		a.ParentAccountID = &parent.Int64
	}
	if proxy.Valid {
		a.ProxyID = &proxy.Int64
	}
	if rate.Valid {
		a.RateMultiplier = &rate.Float64
	}
	if notes.Valid {
		a.Notes = &notes.String
	}
	rows, err := q.QueryContext(ctx, `SELECT group_id FROM account_groups WHERE account_id=$1 ORDER BY group_id`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var group int64
		if err = rows.Scan(&group); err != nil {
			return err
		}
		a.GroupIDs = append(a.GroupIDs, group)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if expected == "" || service.GovernanceAccountFingerprint(a) != expected {
		return gov.ErrConflict
	}
	return nil
}

// Called inside the native account transaction. Shared target locks block both
// deletion and configuration edits until the full account/group write commits.
func checkGovernanceTargets(ctx context.Context, client *dbent.Client) error {
	targets := service.GovernanceTargetsFromContext(ctx)
	if len(targets) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if err := lockLiveGroups(ctx, client, ids); err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			return gov.ErrConflict
		}
		return err
	}
	groups, err := client.Group.Query().Where(dbgroup.IDIn(ids...)).All(ctx)
	if err != nil {
		return err
	}
	if len(groups) != len(ids) {
		return gov.ErrConflict
	}
	for _, group := range groups {
		if targets[group.ID] == "" || service.GovernanceGroupFingerprint(groupEntityToService(group)) != targets[group.ID] {
			return gov.ErrConflict
		}
	}
	return nil
}
func lockAccountForGroupBind(ctx context.Context, q sqlQueryer, id int64) error {
	var got int64
	return scanSingleRow(ctx, q, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, []any{id}, &got)
}
func replaceAccountGroupsInTransaction(ctx context.Context, client *dbent.Client, id int64, groups []int64) error {
	if err := lockLiveGroups(ctx, client, groups); err != nil {
		return err
	}
	if _, err := client.AccountGroup.Delete().Where(dbaccountgroup.AccountIDEQ(id)).Exec(ctx); err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	builders := make([]*dbent.AccountGroupCreate, len(groups))
	for i, group := range groups {
		builders[i] = client.AccountGroup.Create().SetAccountID(id).SetGroupID(group).SetPriority(i + 1)
	}
	_, err := client.AccountGroup.CreateBulk(builders...).Save(ctx)
	return err
}
