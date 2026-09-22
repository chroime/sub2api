//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newChannelMonitorSortRepository(t *testing.T) (*channelMonitorRepository, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	return &channelMonitorRepository{client: client, db: db}, db
}

func createChannelMonitorSortFixture(t *testing.T, repo *channelMonitorRepository, name string, enabled bool) *service.ChannelMonitor {
	t.Helper()
	m := &service.ChannelMonitor{
		Name: name, Provider: service.MonitorProviderOpenAI,
		APIMode:  service.MonitorAPIModeChatCompletions,
		Endpoint: "https://example.com", APIKey: "encrypted-fixture-key",
		PrimaryModel: "fixture-model", Enabled: enabled, IntervalSeconds: 60,
	}
	require.NoError(t, repo.Create(context.Background(), m))
	return m
}

func channelMonitorSortIDs(items []*service.ChannelMonitor) []int64 {
	ids := make([]int64, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func TestChannelMonitorSortRepositoryListAndEnabledShareOrder(t *testing.T) {
	repo, db := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	first := createChannelMonitorSortFixture(t, repo, "first", true)
	second := createChannelMonitorSortFixture(t, repo, "second", false)
	third := createChannelMonitorSortFixture(t, repo, "third", true)
	_, err := db.Exec("UPDATE channel_monitors SET sort_order = CASE WHEN id = ? THEN 20 ELSE 10 END", second.ID)
	require.NoError(t, err)

	items, total, err := repo.List(ctx, service.ChannelMonitorListParams{Page: 1, PageSize: 2})
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Equal(t, []int64{first.ID, third.ID}, channelMonitorSortIDs(items))
	items, _, err = repo.List(ctx, service.ChannelMonitorListParams{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, []int64{second.ID}, channelMonitorSortIDs(items))

	enabled, err := repo.ListEnabled(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID, third.ID}, channelMonitorSortIDs(enabled))
}

func TestChannelMonitorSortRepositoryReturnsAllMonitorsWithoutPagination(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	empty, err := repo.ListSortOrder(ctx)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
	var expected []int64
	for i := 0; i < 205; i++ {
		monitor := createChannelMonitorSortFixture(t, repo, fmt.Sprintf("monitor-%03d", i), i%2 == 0)
		expected = append(expected, monitor.ID)
	}
	items, err := repo.ListSortOrder(ctx)
	require.NoError(t, err)
	require.Len(t, items, 205, "sorting must include every page and disabled monitors")
	for i, item := range items {
		require.Equal(t, expected[i], item.ID)
		require.Equal(t, i, item.SortOrder)
		require.Equal(t, i%2 == 0, item.Enabled)
	}
}

func TestChannelMonitorSortRepositoryPersistsOrderAndKeepsConcurrentEdits(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	first := createChannelMonitorSortFixture(t, repo, "first", true)
	second := createChannelMonitorSortFixture(t, repo, "second", true)
	stale, err := repo.GetByID(ctx, first.ID)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateSortOrders(ctx, []service.ChannelMonitorSortOrderUpdate{
		{ID: first.ID, SortOrder: 40}, {ID: second.ID, SortOrder: 10},
	}))
	// A previously loaded editor must not overwrite a more recent sort change.
	stale.Name = "edited concurrently"
	require.NoError(t, repo.Update(ctx, stale))
	loaded, err := repo.GetByID(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, 40, loaded.SortOrder)
	require.Equal(t, "edited concurrently", loaded.Name)
	require.Equal(t, first.APIKey, loaded.APIKey)
	require.Equal(t, first.Endpoint, loaded.Endpoint)
	items, _, err := repo.List(ctx, service.ChannelMonitorListParams{PageSize: 100})
	require.NoError(t, err)
	require.Equal(t, []int64{second.ID, first.ID}, channelMonitorSortIDs(items))
}

func TestChannelMonitorSortRepositoryMissingMonitorRollsBackWholeBatch(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	first := createChannelMonitorSortFixture(t, repo, "first", true)
	second := createChannelMonitorSortFixture(t, repo, "second", true)
	err := repo.UpdateSortOrders(ctx, []service.ChannelMonitorSortOrderUpdate{
		{ID: first.ID, SortOrder: 90}, {ID: second.ID, SortOrder: 80}, {ID: 999999, SortOrder: 70},
	})
	require.ErrorIs(t, err, service.ErrChannelMonitorNotFound)
	for _, original := range []*service.ChannelMonitor{first, second} {
		loaded, loadErr := repo.GetByID(ctx, original.ID)
		require.NoError(t, loadErr)
		require.Equal(t, original.SortOrder, loaded.SortOrder)
	}
}

func TestChannelMonitorSortRepositoryRejectsInvalidBatchWithoutMutation(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	first := createChannelMonitorSortFixture(t, repo, "first", true)
	for _, updates := range [][]service.ChannelMonitorSortOrderUpdate{
		nil,
		{{ID: first.ID, SortOrder: 10}, {ID: first.ID, SortOrder: 20}},
		{{ID: first.ID, SortOrder: 10}, {ID: 0, SortOrder: 20}},
		{{ID: first.ID, SortOrder: -1}},
	} {
		err := repo.UpdateSortOrders(ctx, updates)
		require.ErrorIs(t, err, service.ErrChannelMonitorInvalidSortOrder)
		loaded, err := repo.GetByID(ctx, first.ID)
		require.NoError(t, err)
		require.Equal(t, first.SortOrder, loaded.SortOrder)
	}
}

func TestChannelMonitorSortRepositoryDatabaseFailureRollsBack(t *testing.T) {
	repo, db := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	first := createChannelMonitorSortFixture(t, repo, "first", true)
	second := createChannelMonitorSortFixture(t, repo, "second", true)
	_, err := db.Exec(fmt.Sprintf(`CREATE TRIGGER reject_monitor_sort BEFORE UPDATE OF sort_order ON channel_monitors
		WHEN NEW.id = %d BEGIN SELECT RAISE(ABORT, 'fixture sort failure'); END`, second.ID))
	require.NoError(t, err)
	err = repo.UpdateSortOrders(ctx, []service.ChannelMonitorSortOrderUpdate{
		{ID: first.ID, SortOrder: 90}, {ID: second.ID, SortOrder: 80},
	})
	require.ErrorContains(t, err, "fixture sort failure")
	loaded, err := repo.GetByID(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, first.SortOrder, loaded.SortOrder)
}

type channelMonitorSortTestEncryptor struct{}

func (channelMonitorSortTestEncryptor) Encrypt(value string) (string, error) {
	return "encrypted:" + value, nil
}
func (channelMonitorSortTestEncryptor) Decrypt(value string) (string, error) {
	return strings.TrimPrefix(value, "encrypted:"), nil
}

func TestChannelMonitorSortRepositoryNewAndDuplicatedMonitorsAppend(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	first := createChannelMonitorSortFixture(t, repo, "first", true)
	second := createChannelMonitorSortFixture(t, repo, "second", true)
	require.NoError(t, repo.UpdateSortOrders(ctx, []service.ChannelMonitorSortOrderUpdate{
		{ID: first.ID, SortOrder: 20}, {ID: second.ID, SortOrder: 40},
	}))
	created := createChannelMonitorSortFixture(t, repo, "third", true)
	require.Equal(t, 41, created.SortOrder)
	svc := service.NewChannelMonitorService(repo, channelMonitorSortTestEncryptor{})
	duplicate, err := svc.Duplicate(ctx, first.ID, 1, "fixture-actor", "fixture-operation")
	require.NoError(t, err)
	require.Equal(t, 42, duplicate.SortOrder)
	require.False(t, duplicate.Enabled)
	items, _, err := repo.List(ctx, service.ChannelMonitorListParams{PageSize: 100})
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID, second.ID, created.ID, duplicate.ID}, channelMonitorSortIDs(items))
}

func TestChannelMonitorSortRepositorySaturatedOrderStillAppendsNewMonitors(t *testing.T) {
	repo, db := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	first := createChannelMonitorSortFixture(t, repo, "first", true)
	_, err := db.Exec("UPDATE channel_monitors SET sort_order = 2147483647 WHERE id = ?", first.ID)
	require.NoError(t, err)
	second := createChannelMonitorSortFixture(t, repo, "second", true)
	third := createChannelMonitorSortFixture(t, repo, "third", true)
	require.Equal(t, 2147483647, second.SortOrder)
	require.Equal(t, 2147483647, third.SortOrder)
	items, _, err := repo.List(ctx, service.ChannelMonitorListParams{PageSize: 100})
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID, second.ID, third.ID}, channelMonitorSortIDs(items))
	enabled, err := repo.ListEnabled(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID, second.ID, third.ID}, channelMonitorSortIDs(enabled))
	sortItems, err := repo.ListSortOrder(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID, second.ID, third.ID}, []int64{sortItems[0].ID, sortItems[1].ID, sortItems[2].ID})
}

func TestChannelMonitorSortRepositorySortCannotOvertakePendingCreate(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	first := createChannelMonitorSortFixture(t, repo, "first", true)
	createReached := make(chan struct{})
	allowCreate := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(allowCreate) }) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	repo.client.ChannelMonitor.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			close(createReached)
			select {
			case <-allowCreate:
				return next.Mutate(ctx, mutation)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
	})
	created := *first
	created.ID, created.Name = 0, "second"
	createDone := make(chan error, 1)
	go func() { createDone <- repo.Create(ctx, &created) }()
	select {
	case <-createReached:
	case <-ctx.Done():
		t.Fatal("create did not reach its insert")
	}
	sortDone := make(chan error, 1)
	go func() {
		sortDone <- repo.UpdateSortOrders(ctx, []service.ChannelMonitorSortOrderUpdate{{ID: first.ID, SortOrder: 10}})
	}()
	select {
	case err := <-sortDone:
		require.NoError(t, err)
		t.Fatal("sorting committed between reading the last rank and inserting the new monitor")
	case <-time.After(100 * time.Millisecond):
	}
	release.Do(func() { close(allowCreate) })
	require.NoError(t, <-createDone)
	require.NoError(t, <-sortDone)
}
