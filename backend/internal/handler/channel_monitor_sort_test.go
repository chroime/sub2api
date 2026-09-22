//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sortChannelMonitorUserRepo struct {
	service.ChannelMonitorRepository
}

func (*sortChannelMonitorUserRepo) ListEnabled(context.Context) ([]*service.ChannelMonitor, error) {
	return []*service.ChannelMonitor{
		{ID: 41, Name: "Zulu", Provider: service.MonitorProviderOpenAI, Enabled: true, PrimaryModel: "model-z", SortOrder: 0, APIKey: "secret-key", Endpoint: "https://private.example.com", ExtraHeaders: map[string]string{"Authorization": "private-token"}},
		{ID: 3, Name: "Alpha", Provider: service.MonitorProviderAnthropic, Enabled: true, PrimaryModel: "model-a", SortOrder: 1},
		{ID: 17, Name: "Middle", Provider: service.MonitorProviderOpenAI, Enabled: true, PrimaryModel: "model-m", SortOrder: 2},
	}, nil
}

func (*sortChannelMonitorUserRepo) ListLatestForMonitorIDs(context.Context, []int64) (map[int64][]*service.ChannelMonitorLatest, error) {
	return map[int64][]*service.ChannelMonitorLatest{}, nil
}

func (*sortChannelMonitorUserRepo) ComputeAvailabilityForMonitors(context.Context, []int64, int) (map[int64][]*service.ChannelMonitorAvailability, error) {
	return map[int64][]*service.ChannelMonitorAvailability{}, nil
}

func (*sortChannelMonitorUserRepo) ListRecentHistoryForMonitors(context.Context, []int64, map[int64]string, int) (map[int64][]*service.ChannelMonitorHistoryEntry, error) {
	return map[int64][]*service.ChannelMonitorHistoryEntry{}, nil
}

func TestChannelMonitorSortOrderUserViewPreservesRepositoryOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewChannelMonitorUserHandler(service.NewChannelMonitorService(&sortChannelMonitorUserRepo{}, nil), nil)
	t.Run("authenticated", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/channel-monitors", nil)

		handler.List(ctx)

		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Data struct {
				Items []struct {
					ID   int64  `json:"id"`
					Name string `json:"name"`
				} `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		require.Len(t, response.Data.Items, 3)
		items := response.Data.Items
		require.Equal(t, []int64{41, 3, 17}, []int64{items[0].ID, items[1].ID, items[2].ID})
		require.Equal(t, []string{"Zulu", "Alpha", "Middle"}, []string{items[0].Name, items[1].Name, items[2].Name})
		for _, sensitive := range []string{"secret-key", "private.example.com", "private-token", "api_key", "endpoint", "extra_headers", "account_id"} {
			require.NotContains(t, recorder.Body.String(), sensitive)
		}
	})
}
