package repository

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *channelMonitorRepository) ListSortOrder(ctx context.Context) ([]service.ChannelMonitorSortOrderItem, error) {
	rows, err := clientFromContext(ctx, r.client).ChannelMonitor.Query().
		Select(channelmonitor.FieldID, channelmonitor.FieldName, channelmonitor.FieldProvider, channelmonitor.FieldEnabled, channelmonitor.FieldSortOrder).
		Order(dbent.Asc(channelmonitor.FieldSortOrder), dbent.Asc(channelmonitor.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channel monitor sort orders: %w", err)
	}
	items := make([]service.ChannelMonitorSortOrderItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, service.ChannelMonitorSortOrderItem{
			ID: row.ID, Name: row.Name, Provider: string(row.Provider),
			Enabled: row.Enabled, SortOrder: row.SortOrder,
		})
	}
	return items, nil
}

func (r *channelMonitorRepository) UpdateSortOrders(ctx context.Context, updates []service.ChannelMonitorSortOrderUpdate) error {
	if err := service.ValidateChannelMonitorSortOrderUpdates(updates); err != nil {
		return err
	}
	return r.withChannelMonitorSortTransaction(ctx, func(txCtx context.Context, client *dbent.Client) error {
		return updateChannelMonitorSortOrders(txCtx, client, updates)
	})
}

func (r *channelMonitorRepository) withChannelMonitorSortTransaction(ctx context.Context, fn func(context.Context, *dbent.Client) error) error {
	tx := dbent.TxFromContext(ctx)
	ownsTransaction := tx == nil
	if ownsTransaction {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return fmt.Errorf("begin channel monitor sort transaction: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	// The shared helper takes a PostgreSQL advisory transaction lock for this
	// monitor-only scope. SQLite tests use its local lock and a DB transaction.
	// Hold the lock through commit so MAX(sort_order)+INSERT cannot interleave
	// with another create or a reordered list.
	release, err := lockRepositoryScopedKeys(txCtx, client, client, "channel_monitors:sort_order")
	if err != nil {
		return fmt.Errorf("lock channel monitor sort order: %w", err)
	}
	defer release()
	if err := fn(txCtx, client); err != nil {
		return err
	}
	if ownsTransaction {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit channel monitor sort transaction: %w", err)
		}
	}
	return nil
}

func updateChannelMonitorSortOrders(ctx context.Context, client *dbent.Client, updates []service.ChannelMonitorSortOrderUpdate) error {
	args := make([]any, 0, len(updates)*2)
	cases := make([]string, 0, len(updates))
	ids := make([]string, 0, len(updates))
	for _, update := range updates {
		idParam := fmt.Sprintf("$%d", len(args)+1)
		orderParam := fmt.Sprintf("$%d", len(args)+2)
		cases = append(cases, "WHEN "+idParam+" THEN "+orderParam)
		ids = append(ids, idParam)
		args = append(args, update.ID, update.SortOrder)
	}
	// Only change ordering: a concurrent edit must not have any of its monitor
	// configuration, credentials or runtime state overwritten by a stale copy.
	query := "UPDATE channel_monitors SET sort_order = CASE id " + strings.Join(cases, " ") +
		" ELSE sort_order END WHERE id IN (" + strings.Join(ids, ", ") + ")"
	result, err := client.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update channel monitor sort orders: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count updated channel monitor sort orders: %w", err)
	}
	if affected != int64(len(updates)) {
		return service.ErrChannelMonitorNotFound
	}
	return nil
}
