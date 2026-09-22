//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorAdminResponseIncludesSortOrder(t *testing.T) {
	for _, rank := range []int{0, 29} {
		encoded, err := json.Marshal(channelMonitorToResponse(&service.ChannelMonitor{ID: 7, Enabled: true, SortOrder: rank}))
		require.NoError(t, err)
		var item map[string]any
		require.NoError(t, json.Unmarshal(encoded, &item))
		require.Contains(t, item, "sort_order", "admin responses must expose the persisted order even when it is zero")
		require.Equal(t, float64(rank), item["sort_order"])
	}
}

type sortChannelMonitorAdminRepo struct {
	service.ChannelMonitorRepository
	items     []service.ChannelMonitorSortOrderItem
	listErr   error
	updateErr error
}

func (r *sortChannelMonitorAdminRepo) ListSortOrder(context.Context) ([]service.ChannelMonitorSortOrderItem, error) {
	return r.items, r.listErr
}

func (r *sortChannelMonitorAdminRepo) UpdateSortOrders(_ context.Context, updates []service.ChannelMonitorSortOrderUpdate) error {
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

func runSortChannelMonitorAdminRequest(t *testing.T, action gin.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	action(ctx)
	return recorder
}

func TestChannelMonitorSortOrderAdminListReturnsSafeUnpaginatedProjection(t *testing.T) {
	items := make([]service.ChannelMonitorSortOrderItem, 105)
	for i := range items {
		provider := service.MonitorProviderOpenAI
		if i%2 != 0 {
			provider = service.MonitorProviderAnthropic
		}
		items[i] = service.ChannelMonitorSortOrderItem{
			ID: int64(105 - i), Name: "monitor-" + strconv.Itoa(i), Provider: provider, Enabled: i%2 == 0, SortOrder: i,
		}
	}
	handler := NewChannelMonitorHandler(service.NewChannelMonitorService(&sortChannelMonitorAdminRepo{items: items}, nil))

	recorder := runSortChannelMonitorAdminRequest(t, handler.ListSortOrder, http.MethodGet,
		"/api/v1/admin/channel-monitors/sort-order?page=2&page_size=1&provider=openai&enabled=true&search=missing", "")

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 105, "the ordering dialog needs every monitor, including disabled rows and other providers")
	for _, item := range response.Data {
		require.Len(t, item, 5, "sort-order items must not expose credentials, endpoints, headers or runtime details")
		for _, key := range []string{"id", "name", "provider", "enabled", "sort_order"} {
			require.Contains(t, item, key)
		}
	}
	require.Equal(t, map[string]any{"id": float64(105), "name": "monitor-0", "provider": "openai", "enabled": true, "sort_order": float64(0)}, response.Data[0])
	require.Equal(t, map[string]any{"id": float64(104), "name": "monitor-1", "provider": "anthropic", "enabled": false, "sort_order": float64(1)}, response.Data[1])
	require.Equal(t, float64(1), response.Data[104]["id"])
	require.Equal(t, float64(104), response.Data[104]["sort_order"])
}

func TestChannelMonitorSortOrderAdminListEmptyIsArray(t *testing.T) {
	handler := NewChannelMonitorHandler(service.NewChannelMonitorService(&sortChannelMonitorAdminRepo{}, nil))
	recorder := runSortChannelMonitorAdminRequest(t, handler.ListSortOrder, http.MethodGet, "/sort-order", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":[]}`, recorder.Body.String())
}

func TestChannelMonitorSortOrderAdminUpdatePersistsRequestedRanks(t *testing.T) {
	repo := &sortChannelMonitorAdminRepo{items: []service.ChannelMonitorSortOrderItem{
		{ID: 4, Name: "Disabled", Provider: service.MonitorProviderOpenAI, Enabled: false, SortOrder: 5},
		{ID: 9, Name: "Enabled", Provider: service.MonitorProviderAnthropic, Enabled: true, SortOrder: 8},
	}}
	handler := NewChannelMonitorHandler(service.NewChannelMonitorService(repo, nil))

	recorder := runSortChannelMonitorAdminRequest(t, handler.UpdateSortOrder, http.MethodPut, "/sort-order",
		`{"updates":[{"id":9,"sort_order":0},{"id":4,"sort_order":2147483646}]}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data.Message)
	readback := runSortChannelMonitorAdminRequest(t, handler.ListSortOrder, http.MethodGet, "/sort-order", "")
	require.Equal(t, http.StatusOK, readback.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":[
		{"id":4,"name":"Disabled","provider":"openai","enabled":false,"sort_order":2147483646},
		{"id":9,"name":"Enabled","provider":"anthropic","enabled":true,"sort_order":0}
	]}`, readback.Body.String())
}

func TestChannelMonitorSortOrderAdminRejectsInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing body", body: ""},
		{name: "malformed JSON", body: "{"},
		{name: "missing updates", body: `{}`},
		{name: "null updates", body: `{"updates":null}`},
		{name: "empty updates", body: `{"updates":[]}`},
		{name: "zero ID", body: `{"updates":[{"id":0,"sort_order":0}]}`},
		{name: "negative ID", body: `{"updates":[{"id":-2,"sort_order":0}]}`},
		{name: "negative rank", body: `{"updates":[{"id":2,"sort_order":-1}]}`},
		{name: "overflow on append", body: `{"updates":[{"id":2,"sort_order":2147483647}]}`},
		{name: "duplicate IDs", body: `{"updates":[{"id":2,"sort_order":0},{"id":2,"sort_order":1}]}`},
		{name: "fractional rank", body: `{"updates":[{"id":2,"sort_order":1.5}]}`},
		{name: "nonnumeric rank", body: `{"updates":[{"id":2,"sort_order":"first"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewChannelMonitorHandler(service.NewChannelMonitorService(&sortChannelMonitorAdminRepo{items: []service.ChannelMonitorSortOrderItem{
				{ID: 2, Name: "Existing", Provider: service.MonitorProviderOpenAI, Enabled: true, SortOrder: 8},
			}}, nil))
			recorder := runSortChannelMonitorAdminRequest(t, handler.UpdateSortOrder, http.MethodPut, "/sort-order", test.body)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			readback := runSortChannelMonitorAdminRequest(t, handler.ListSortOrder, http.MethodGet, "/sort-order", "")
			require.JSONEq(t, `{"code":0,"message":"success","data":[{"id":2,"name":"Existing","provider":"openai","enabled":true,"sort_order":8}]}`, readback.Body.String())
		})
	}
}

func TestChannelMonitorSortOrderAdminMapsRepositoryErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "missing monitor", err: service.ErrChannelMonitorNotFound, status: http.StatusNotFound},
		{name: "invalid sort order", err: service.ErrChannelMonitorInvalidSortOrder, status: http.StatusBadRequest},
		{name: "storage failure", err: errors.New("private storage failure"), status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewChannelMonitorHandler(service.NewChannelMonitorService(&sortChannelMonitorAdminRepo{updateErr: test.err}, nil))
			recorder := runSortChannelMonitorAdminRequest(t, handler.UpdateSortOrder, http.MethodPut, "/sort-order", `{"updates":[{"id":2,"sort_order":0}]}`)
			require.Equal(t, test.status, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "private storage failure")
		})
	}

	handler := NewChannelMonitorHandler(service.NewChannelMonitorService(&sortChannelMonitorAdminRepo{listErr: errors.New("private storage failure")}, nil))
	recorder := runSortChannelMonitorAdminRequest(t, handler.ListSortOrder, http.MethodGet, "/sort-order", "")
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "private storage failure")
}
