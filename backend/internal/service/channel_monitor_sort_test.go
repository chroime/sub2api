//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type sortChannelMonitorServiceRepo struct {
	ChannelMonitorRepository
	items     []ChannelMonitorSortOrderItem
	monitors  []*ChannelMonitor
	updateErr error
}

func (r *sortChannelMonitorServiceRepo) ListSortOrder(context.Context) ([]ChannelMonitorSortOrderItem, error) {
	return append([]ChannelMonitorSortOrderItem{}, r.items...), nil
}

func (r *sortChannelMonitorServiceRepo) UpdateSortOrders(_ context.Context, updates []ChannelMonitorSortOrderUpdate) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for _, update := range updates {
		for i := range r.items {
			if r.items[i].ID == update.ID {
				r.items[i].SortOrder = update.SortOrder
			}
		}
	}
	return nil
}

func (r *sortChannelMonitorServiceRepo) ListEnabled(context.Context) ([]*ChannelMonitor, error) {
	return r.monitors, nil
}

func (*sortChannelMonitorServiceRepo) ListLatestForMonitorIDs(context.Context, []int64) (map[int64][]*ChannelMonitorLatest, error) {
	return map[int64][]*ChannelMonitorLatest{}, nil
}

func (*sortChannelMonitorServiceRepo) ComputeAvailabilityForMonitors(context.Context, []int64, int) (map[int64][]*ChannelMonitorAvailability, error) {
	return map[int64][]*ChannelMonitorAvailability{}, nil
}

func (*sortChannelMonitorServiceRepo) ListRecentHistoryForMonitors(context.Context, []int64, map[int64]string, int) (map[int64][]*ChannelMonitorHistoryEntry, error) {
	return map[int64][]*ChannelMonitorHistoryEntry{}, nil
}

func TestChannelMonitorSortOrderRejectsInvalidUpdatesBeforeChangingOrder(t *testing.T) {
	for _, test := range []struct {
		name    string
		updates []ChannelMonitorSortOrderUpdate
	}{
		{name: "nil"},
		{name: "empty", updates: []ChannelMonitorSortOrderUpdate{}},
		{name: "zero ID", updates: []ChannelMonitorSortOrderUpdate{{ID: 0, SortOrder: 0}}},
		{name: "negative ID", updates: []ChannelMonitorSortOrderUpdate{{ID: -2, SortOrder: 0}}},
		{name: "negative order", updates: []ChannelMonitorSortOrderUpdate{{ID: 2, SortOrder: -1}}},
		{name: "order leaves no room for append", updates: []ChannelMonitorSortOrderUpdate{{ID: 2, SortOrder: 2147483647}}},
		{name: "duplicate ID", updates: []ChannelMonitorSortOrderUpdate{{ID: 2, SortOrder: 0}, {ID: 2, SortOrder: 1}}},
		{name: "duplicate identical entry", updates: []ChannelMonitorSortOrderUpdate{{ID: 2, SortOrder: 0}, {ID: 2, SortOrder: 0}}},
		{name: "invalid entry after valid entry", updates: []ChannelMonitorSortOrderUpdate{{ID: 2, SortOrder: 4}, {ID: 3, SortOrder: -1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &sortChannelMonitorServiceRepo{items: []ChannelMonitorSortOrderItem{
				{ID: 2, Name: "Second", Provider: MonitorProviderOpenAI, Enabled: true, SortOrder: 8},
				{ID: 3, Name: "Third", Provider: MonitorProviderAnthropic, Enabled: false, SortOrder: 9},
			}}
			service := NewChannelMonitorService(repo, nil)

			err := service.UpdateSortOrders(context.Background(), test.updates)

			require.ErrorIs(t, err, ErrChannelMonitorInvalidSortOrder)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
			items, err := service.ListSortOrder(context.Background())
			require.NoError(t, err)
			require.Equal(t, []ChannelMonitorSortOrderItem{
				{ID: 2, Name: "Second", Provider: MonitorProviderOpenAI, Enabled: true, SortOrder: 8},
				{ID: 3, Name: "Third", Provider: MonitorProviderAnthropic, Enabled: false, SortOrder: 9},
			}, items, "an invalid batch must leave every saved rank unchanged")
		})
	}
}

func TestChannelMonitorSortOrderAcceptsZeroTiesAndMaximumRank(t *testing.T) {
	repo := &sortChannelMonitorServiceRepo{items: []ChannelMonitorSortOrderItem{
		{ID: 4, Name: "Fourth", Provider: MonitorProviderOpenAI, Enabled: false, SortOrder: 8},
		{ID: 8, Name: "Eighth", Provider: MonitorProviderAnthropic, Enabled: true, SortOrder: 9},
		{ID: 9, Name: "Ninth", Provider: MonitorProviderOpenAI, Enabled: true, SortOrder: 10},
		{ID: 12, Name: "Unchanged", Provider: MonitorProviderOpenAI, Enabled: true, SortOrder: 11},
	}}
	service := NewChannelMonitorService(repo, nil)

	err := service.UpdateSortOrders(context.Background(), []ChannelMonitorSortOrderUpdate{
		{ID: 9, SortOrder: 0}, {ID: 4, SortOrder: 0}, {ID: 8, SortOrder: 2147483646},
	})
	require.NoError(t, err)
	items, err := service.ListSortOrder(context.Background())
	require.NoError(t, err)
	require.Equal(t, []ChannelMonitorSortOrderItem{
		{ID: 4, Name: "Fourth", Provider: MonitorProviderOpenAI, Enabled: false, SortOrder: 0},
		{ID: 8, Name: "Eighth", Provider: MonitorProviderAnthropic, Enabled: true, SortOrder: 2147483646},
		{ID: 9, Name: "Ninth", Provider: MonitorProviderOpenAI, Enabled: true, SortOrder: 0},
		{ID: 12, Name: "Unchanged", Provider: MonitorProviderOpenAI, Enabled: true, SortOrder: 11},
	}, items)
}

func TestChannelMonitorSortOrderPreservesRepositoryErrors(t *testing.T) {
	for _, repositoryErr := range []error{ErrChannelMonitorNotFound, errors.New("storage unavailable")} {
		service := NewChannelMonitorService(&sortChannelMonitorServiceRepo{updateErr: repositoryErr}, nil)
		err := service.UpdateSortOrders(context.Background(), []ChannelMonitorSortOrderUpdate{{ID: 2, SortOrder: 0}})
		require.ErrorIs(t, err, repositoryErr)
	}
}

func TestChannelMonitorSortOrderUserViewPreservesRepositoryOrder(t *testing.T) {
	repo := &sortChannelMonitorServiceRepo{monitors: []*ChannelMonitor{
		{ID: 41, Name: "Zulu", Provider: MonitorProviderOpenAI, Enabled: true, PrimaryModel: "model-z", SortOrder: 0},
		{ID: 3, Name: "Alpha", Provider: MonitorProviderAnthropic, Enabled: true, PrimaryModel: "model-a", SortOrder: 1},
		{ID: 17, Name: "Middle", Provider: MonitorProviderOpenAI, Enabled: true, PrimaryModel: "model-m", SortOrder: 2},
	}}
	service := NewChannelMonitorService(repo, nil)

	views, err := service.ListUserView(context.Background())

	require.NoError(t, err)
	require.Len(t, views, 3)
	require.Equal(t, []int64{41, 3, 17}, []int64{views[0].ID, views[1].ID, views[2].ID})
	require.Equal(t, []string{"Zulu", "Alpha", "Middle"}, []string{views[0].Name, views[1].Name, views[2].Name})
	require.Equal(t, []string{"model-z", "model-a", "model-m"}, []string{views[0].PrimaryModel, views[1].PrimaryModel, views[2].PrimaryModel})
}
