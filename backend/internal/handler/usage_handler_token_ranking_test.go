package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tokenRankingRepoStub struct {
	service.UsageLogRepository
	rows      []usagestats.UserBreakdownItem
	start     time.Time
	end       time.Time
	dimension usagestats.UserBreakdownDimension
	limit     int
}

func (s *tokenRankingRepoStub) GetUserBreakdownStats(_ context.Context, start, end time.Time, dimension usagestats.UserBreakdownDimension, limit int) ([]usagestats.UserBreakdownItem, error) {
	s.start = start
	s.end = end
	s.dimension = dimension
	s.limit = limit
	return s.rows, nil
}

func newTokenRankingTestRouter(repo *tokenRankingRepoStub, userID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
		c.Next()
	})
	router.GET("/user/token-ranking", handler.GetTokenRanking)
	return router
}

func TestGetTokenRankingUsesBeijingTodayTop20AndMasksEmail(t *testing.T) {
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	today := timezone.Today()
	repo := &tokenRankingRepoStub{rows: []usagestats.UserBreakdownItem{
		{UserID: 7, Email: "alice@example.com", Requests: 2, InputTokens: 12, OutputTokens: 8, CacheTokens: 3, TotalTokens: 23},
	}}
	router := newTokenRankingTestRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/user/token-ranking?period=today", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, today.Equal(repo.start))
	require.True(t, today.AddDate(0, 0, 1).Equal(repo.end))
	_, offset := repo.start.Zone()
	require.Equal(t, 8*60*60, offset)
	require.Equal(t, "total_tokens", repo.dimension.SortBy)
	require.Equal(t, 20, repo.limit)

	var envelope struct {
		Data struct {
			Period      string `json:"period"`
			TotalTokens int64  `json:"total_tokens"`
			GeneratedAt string `json:"generated_at"`
			Ranking     []struct {
				Rank        int    `json:"rank"`
				UserID      int64  `json:"user_id"`
				Email       string `json:"email"`
				TotalTokens int64  `json:"total_tokens"`
			} `json:"ranking"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, "today", envelope.Data.Period)
	require.Len(t, envelope.Data.Ranking, 1)
	require.Equal(t, 1, envelope.Data.Ranking[0].Rank)
	require.Equal(t, int64(7), envelope.Data.Ranking[0].UserID)
	require.Equal(t, "a***e@example.com", envelope.Data.Ranking[0].Email)
	require.Equal(t, int64(23), envelope.Data.Ranking[0].TotalTokens)
	require.Equal(t, int64(23), envelope.Data.TotalTokens)
	require.Contains(t, envelope.Data.GeneratedAt, "+08:00")
}

func TestGetTokenRankingSupportsFixedBeijingPeriods(t *testing.T) {
	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, beijing)
	today := time.Date(2026, 10, 4, 0, 0, 0, 0, beijing)
	tests := []struct {
		period    string
		wantStart time.Time
		wantEnd   time.Time
	}{
		{period: "today", wantStart: today, wantEnd: today.AddDate(0, 0, 1)},
		{period: "yesterday", wantStart: today.AddDate(0, 0, -1), wantEnd: today},
		{period: "7d", wantStart: today.AddDate(0, 0, -6), wantEnd: today.AddDate(0, 0, 1)},
		{period: "30d", wantStart: today.AddDate(0, 0, -29), wantEnd: today.AddDate(0, 0, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			start, end, ok := tokenRankingRange(tt.period, now)
			require.True(t, ok)
			require.True(t, tt.wantStart.Equal(start))
			require.True(t, tt.wantEnd.Equal(end))
			_, offset := start.Zone()
			require.Equal(t, 8*60*60, offset)
		})
	}
}

func TestGetTokenRankingRejectsUnknownOrRepeatedPeriod(t *testing.T) {
	for _, query := range []string{
		"?period=week",
		"?period=today&period=30d",
	} {
		t.Run(query, func(t *testing.T) {
			router := newTokenRankingTestRouter(&tokenRankingRepoStub{}, 42)
			req := httptest.NewRequest(http.MethodGet, "/user/token-ranking"+query, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestGetTokenRankingCapsProjectionAtTwentyRows(t *testing.T) {
	rows := make([]usagestats.UserBreakdownItem, 25)
	for i := range rows {
		rows[i] = usagestats.UserBreakdownItem{UserID: int64(i + 1), TotalTokens: int64(25 - i)}
	}
	router := newTokenRankingTestRouter(&tokenRankingRepoStub{rows: rows}, 42)
	req := httptest.NewRequest(http.MethodGet, "/user/token-ranking?period=30d", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var envelope struct {
		Data struct {
			Ranking []json.RawMessage `json:"ranking"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Ranking, tokenRankingLimit)
}
