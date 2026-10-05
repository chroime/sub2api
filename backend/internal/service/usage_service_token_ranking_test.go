package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type tokenRankingUsageRepoStub struct {
	UsageLogRepository
	rows      []usagestats.UserBreakdownItem
	err       error
	start     time.Time
	end       time.Time
	dimension usagestats.UserBreakdownDimension
	limit     int
}

func (s *tokenRankingUsageRepoStub) GetUserBreakdownStats(_ context.Context, start, end time.Time, dimension usagestats.UserBreakdownDimension, limit int) ([]usagestats.UserBreakdownItem, error) {
	s.start = start
	s.end = end
	s.dimension = dimension
	s.limit = limit
	return s.rows, s.err
}

func TestUsageServiceGetUserBreakdownStatsDelegatesToRepository(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	end := start.AddDate(0, 0, 1)
	dimension := usagestats.UserBreakdownDimension{SortBy: "total_tokens"}
	want := []usagestats.UserBreakdownItem{{UserID: 7, TotalTokens: 100}}
	repo := &tokenRankingUsageRepoStub{rows: want}
	service := NewUsageService(repo, nil, nil, nil)

	got, err := service.GetUserBreakdownStats(context.Background(), start, end, dimension, 20)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.True(t, start.Equal(repo.start))
	require.True(t, end.Equal(repo.end))
	require.Equal(t, dimension, repo.dimension)
	require.Equal(t, 20, repo.limit)
}

func TestUsageServiceGetUserBreakdownStatsWrapsRepositoryError(t *testing.T) {
	wantErr := errors.New("database unavailable")
	service := NewUsageService(&tokenRankingUsageRepoStub{err: wantErr}, nil, nil, nil)

	_, err := service.GetUserBreakdownStats(context.Background(), time.Time{}, time.Time{}, usagestats.UserBreakdownDimension{}, 20)

	require.ErrorIs(t, err, wantErr)
	require.ErrorContains(t, err, "get user breakdown stats")
}
