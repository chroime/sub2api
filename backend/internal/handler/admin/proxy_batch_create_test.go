//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type proxyBatchListCall struct {
	page, pageSize                        int
	protocol, status, search, sort, order string
}

type proxyBatchAdminService struct {
	stubAdminService
	stateMu      sync.Mutex
	listCalls    []proxyBatchListCall
	listErr      error
	createErr    error
	createCalls  int
	failCreateAt int
	beforeCreate func(int)
	afterList    func(int)
}

func (s *proxyBatchAdminService) ListProxies(ctx context.Context, page, pageSize int, protocol, status, search, sort, order string) ([]service.Proxy, int64, error) {
	s.stateMu.Lock()
	s.listCalls = append(s.listCalls, proxyBatchListCall{page, pageSize, protocol, status, search, sort, order})
	if s.afterList != nil {
		s.afterList(len(s.listCalls))
	}
	defer s.stateMu.Unlock()
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	start := min((page-1)*pageSize, len(s.proxies))
	end := min(start+pageSize, len(s.proxies))
	return append([]service.Proxy(nil), s.proxies[start:end]...), int64(len(s.proxies)), nil
}

func (s *proxyBatchAdminService) CheckProxyExists(ctx context.Context, host string, port int, username, password string) (bool, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	for _, proxy := range s.proxies {
		if proxy.Host == host && proxy.Port == port && proxy.Username == username && proxy.Password == password {
			return true, nil
		}
	}
	for _, input := range s.createdProxies {
		if input.Host == host && input.Port == port && input.Username == username && input.Password == password {
			return true, nil
		}
	}
	return false, nil
}

func (s *proxyBatchAdminService) CreateProxy(ctx context.Context, input *service.CreateProxyInput) (*service.Proxy, error) {
	s.stateMu.Lock()
	s.createCalls++
	call := s.createCalls
	s.stateMu.Unlock()
	if s.beforeCreate != nil {
		s.beforeCreate(call)
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.createErr != nil {
		return nil, s.createErr
	}
	if s.failCreateAt == call {
		return nil, errors.New("database unavailable during import")
	}
	proxy := service.Proxy{ID: int64(len(s.proxies) + 1), Name: input.Name, Protocol: input.Protocol, Host: input.Host, Port: input.Port, Username: input.Username, Password: input.Password, Status: service.StatusActive}
	s.createdProxies = append(s.createdProxies, input)
	s.proxies = append(s.proxies, proxy)
	return &proxy, nil
}

func proxyBatchItem(name, host string) map[string]any {
	return map[string]any{"name": name, "protocol": "socks5", "host": host, "port": 1080, "username": "user", "password": "secret"}
}

func requestProxyBatch(t *testing.T, svc service.AdminService, items ...map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(map[string]any{"proxies": items})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/proxies/batch", NewProxyHandler(svc).BatchCreate)
	return serveProxyBatch(router, body)
}

func serveProxyBatch(router http.Handler, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/proxies/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestProxyHandlerBatchCreateUsesSuppliedNames(t *testing.T) {
	svc := &proxyBatchAdminService{}
	w := requestProxyBatch(t, svc, proxyBatchItem(" us001 ", "192.0.2.1"), proxyBatchItem("jp002", "192.0.2.2"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, svc.createdProxies, 2)
	require.Equal(t, "us001", svc.createdProxies[0].Name)
	require.Equal(t, "jp002", svc.createdProxies[1].Name)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"created":2,"skipped":0}}`, w.Body.String())
}

func TestProxyHandlerBatchCreateRejectsInvalidWholeBatch(t *testing.T) {
	cases := []struct {
		name string
		edit func(map[string]any)
	}{
		{"missing name", func(item map[string]any) { delete(item, "name") }},
		{"empty name", func(item map[string]any) { item["name"] = "" }},
		{"whitespace name", func(item map[string]any) { item["name"] = " \t\n " }},
		{"control character", func(item map[string]any) { item["name"] = "us\n002" }},
		{"too many ASCII characters", func(item map[string]any) { item["name"] = strings.Repeat("a", 101) }},
		{"too many Unicode characters", func(item map[string]any) { item["name"] = strings.Repeat("日", 101) }},
		{"duplicate name", func(item map[string]any) { item["name"] = "us001" }},
		{"case insensitive duplicate", func(item map[string]any) { item["name"] = " US001 " }},
		{"invalid protocol", func(item map[string]any) { item["protocol"] = "ftp" }},
		{"missing host", func(item map[string]any) { delete(item, "host") }},
		{"zero port", func(item map[string]any) { item["port"] = 0 }},
		{"port above maximum", func(item map[string]any) { item["port"] = 65536 }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			svc := &proxyBatchAdminService{}
			invalid := proxyBatchItem("us002", "192.0.2.2")
			tt.edit(invalid)
			w := requestProxyBatch(t, svc, proxyBatchItem("us001", "192.0.2.1"), invalid)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Empty(t, svc.createdProxies, "invalid later rows must not leave earlier rows imported")
		})
	}
}

func TestProxyHandlerBatchCreateAllowsOneHundredUnicodeCharacters(t *testing.T) {
	svc := &proxyBatchAdminService{}
	name := strings.Repeat("日", 100)
	w := requestProxyBatch(t, svc, proxyBatchItem(name, "192.0.2.1"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, svc.createdProxies, 1)
	require.Equal(t, name, svc.createdProxies[0].Name)
}

func TestProxyHandlerBatchCreateRejectsExistingNameOnLaterPage(t *testing.T) {
	svc := &proxyBatchAdminService{}
	for i := range 1000 {
		svc.proxies = append(svc.proxies, service.Proxy{ID: int64(i + 1), Name: fmt.Sprintf("old%04d", i), Status: service.StatusActive})
	}
	svc.proxies = append(svc.proxies, service.Proxy{ID: 1001, Name: "US002", Status: "inactive"})
	w := requestProxyBatch(t, svc, proxyBatchItem("us001", "192.0.2.1"), proxyBatchItem("us002", "192.0.2.2"))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Empty(t, svc.createdProxies)
	require.Len(t, svc.listCalls, 2)
	require.Equal(t, proxyBatchListCall{1, 1000, "", "", "", "id", "asc"}, svc.listCalls[0])
	require.Equal(t, proxyBatchListCall{2, 1000, "", "", "", "id", "asc"}, svc.listCalls[1])
}

func TestProxyHandlerBatchCreatePreservesDuplicateEndpointSkip(t *testing.T) {
	svc := &proxyBatchAdminService{stubAdminService: stubAdminService{proxies: []service.Proxy{{
		Name: "existing", Host: "192.0.2.1", Port: 1080, Username: "user", Password: "secret",
	}}}}
	w := requestProxyBatch(t, svc,
		proxyBatchItem("us001", "192.0.2.1"),
		proxyBatchItem("us002", "192.0.2.2"),
		proxyBatchItem("us003", "192.0.2.2"),
	)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, svc.createdProxies, 1)
	require.Equal(t, "us002", svc.createdProxies[0].Name)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"created":1,"skipped":2}}`, w.Body.String())
}

func TestProxyHandlerBatchCreateSkipsRepeatedEndpointWithinBatch(t *testing.T) {
	svc := &stubAdminService{}
	w := requestProxyBatch(t, svc, proxyBatchItem("us001", "192.0.2.1"), proxyBatchItem("us002", "192.0.2.1"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, svc.createdProxies, 1)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"created":1,"skipped":1}}`, w.Body.String())
}

func TestProxyHandlerBatchCreateSurfacesCreateFailure(t *testing.T) {
	svc := &proxyBatchAdminService{createErr: errors.New("database unavailable")}
	w := requestProxyBatch(t, svc, proxyBatchItem("us001", "192.0.2.1"))
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"skipped"`)
}

func TestProxyHandlerBatchCreateDoesNotWriteWhenNamesCannotBeChecked(t *testing.T) {
	svc := &proxyBatchAdminService{listErr: errors.New("database unavailable")}
	w := requestProxyBatch(t, svc, proxyBatchItem("us001", "192.0.2.1"))
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.Empty(t, svc.createdProxies)
}

func TestProxyHandlerBatchCreateRetryCompletesAfterPartialFailure(t *testing.T) {
	svc := &proxyBatchAdminService{failCreateAt: 2}
	first := proxyBatchItem("us001", "192.0.2.1")
	second := proxyBatchItem("us002", "192.0.2.2")
	w := requestProxyBatch(t, svc, first, second)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.Len(t, svc.proxies, 1)
	require.Equal(t, "us001", svc.proxies[0].Name)

	// A retry must retain the successful first row and finish the second row.
	w = requestProxyBatch(t, svc, first, second)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"code":0,"message":"success","data":{"created":1,"skipped":1}}`, w.Body.String())
	require.Len(t, svc.createdProxies, 2)
	require.Equal(t, "us001", svc.createdProxies[0].Name)
	require.Equal(t, "us002", svc.createdProxies[1].Name)
}

func TestProxyHandlerBatchCreateRejectsSameNameWithDifferentEndpoint(t *testing.T) {
	for _, field := range []string{"host", "port", "username", "password"} {
		t.Run(field, func(t *testing.T) {
			svc := &proxyBatchAdminService{stubAdminService: stubAdminService{proxies: []service.Proxy{{
				Name: "US001", Host: "192.0.2.1", Port: 1080, Username: "user", Password: "secret",
			}}}}
			item := proxyBatchItem("us001", "192.0.2.1")
			if field == "port" {
				item[field] = 1081
			} else {
				item[field] = "different"
			}
			w := requestProxyBatch(t, svc, item)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Empty(t, svc.createdProxies)
		})
	}
}

func TestProxyHandlerBatchCreateSerializesConcurrentNameReservations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	firstCreating := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondListed := make(chan struct{})
	svc := &proxyBatchAdminService{
		beforeCreate: func(call int) {
			if call == 1 {
				close(firstCreating)
				<-releaseFirst
			}
		},
		afterList: func(call int) {
			if call == 2 {
				close(secondListed)
			}
		},
	}
	router := gin.New()
	router.POST("/proxies/batch", NewProxyHandler(svc).BatchCreate)
	firstBody := []byte(`{"proxies":[{"name":"us001","protocol":"socks5","host":"192.0.2.1","port":1080}]}`)
	secondBody := []byte(`{"proxies":[{"name":"us001","protocol":"socks5","host":"192.0.2.2","port":1080}]}`)
	firstResult := make(chan *httptest.ResponseRecorder, 1)
	secondResult := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstResult <- serveProxyBatch(router, firstBody) }()
	<-firstCreating
	go func() { secondResult <- serveProxyBatch(router, secondBody) }()

	// Without serialization the second request reads an empty inventory while
	// the first write is held; with serialization it waits for that write.
	select {
	case <-secondListed:
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	first := <-firstResult
	second := <-secondResult
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Equal(t, http.StatusBadRequest, second.Code, second.Body.String())
	require.Len(t, svc.createdProxies, 1)
}
