package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Leave one integer value available for appending new monitors.
const MaxChannelMonitorSortOrder = 2147483646

var ErrChannelMonitorInvalidSortOrder = infraerrors.BadRequest(
	"CHANNEL_MONITOR_INVALID_SORT_ORDER",
	"sort updates must contain unique positive monitor IDs and valid non-negative sort orders",
)

func ValidateChannelMonitorSortOrderUpdates(updates []ChannelMonitorSortOrderUpdate) error {
	if len(updates) == 0 {
		return ErrChannelMonitorInvalidSortOrder
	}
	seen := make(map[int64]struct{}, len(updates))
	for _, update := range updates {
		if update.ID <= 0 || update.SortOrder < 0 || update.SortOrder > MaxChannelMonitorSortOrder {
			return ErrChannelMonitorInvalidSortOrder
		}
		if _, exists := seen[update.ID]; exists {
			return ErrChannelMonitorInvalidSortOrder
		}
		seen[update.ID] = struct{}{}
	}
	return nil
}

func (s *ChannelMonitorService) ListSortOrder(ctx context.Context) ([]ChannelMonitorSortOrderItem, error) {
	items, err := s.repo.ListSortOrder(ctx)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []ChannelMonitorSortOrderItem{}
	}
	return items, nil
}

func (s *ChannelMonitorService) UpdateSortOrders(ctx context.Context, updates []ChannelMonitorSortOrderUpdate) error {
	if err := ValidateChannelMonitorSortOrderUpdates(updates); err != nil {
		return err
	}
	return s.repo.UpdateSortOrders(ctx, updates)
}
