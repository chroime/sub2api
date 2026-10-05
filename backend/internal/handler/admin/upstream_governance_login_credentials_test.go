package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type governanceLoginHandlerStore struct {
	gov.Store
	site gov.Site
}

func (s *governanceLoginHandlerStore) GetSite(_ context.Context, id int64) (*gov.Site, error) {
	if id != s.site.ID {
		return nil, gov.ErrNotFound
	}
	site := s.site
	return &site, nil
}
func (s *governanceLoginHandlerStore) ListSites(context.Context) ([]gov.Site, error) {
	return []gov.Site{s.site}, nil
}
func (s *governanceLoginHandlerStore) LockSite(context.Context, int64) (func(), bool, error) {
	return func() {}, true, nil
}
func (s *governanceLoginHandlerStore) UpdateSite(_ context.Context, site *gov.Site, version int64) error {
	if version != s.site.Version {
		return gov.ErrConflict
	}
	site.Version++
	s.site = *site
	return nil
}

func TestGovernanceLoginCredentialsHandlerReadEditAndNoStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	encryptor, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	store := &governanceLoginHandlerStore{site: gov.Site{ID: 1, Name: "Fixture", Platform: "sub2api", BaseURL: "https://fixture.example", Version: 1, IntervalMinutes: 15}}
	h := NewUpstreamGovernanceHandler(gov.NewService(store, nil, nil, encryptor, true))
	router := gin.New()
	router.GET("/sites", h.List)
	router.GET("/sites/:id/login-credentials", h.LoginCredentials)
	router.PUT("/sites/:id", h.Update)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response
	}
	legacy := request("GET", "/sites/1/login-credentials", "")
	require.Equal(t, 200, legacy.Code)
	require.Equal(t, "no-store", legacy.Header().Get("Cache-Control"))
	require.Contains(t, legacy.Body.String(), `"username":"","password":"","version":1`)
	body := `{"name":"Edited","platform":"sub2api","base_url":"https://fixture.example","interval_minutes":15,"version":1,"login_credentials":{"username":"admin-canary","password":"password-canary"}}`
	edited := request("PUT", "/sites/1", body)
	require.Equal(t, 200, edited.Code)
	require.NotContains(t, edited.Body.String(), "password-canary")
	read := request("GET", "/sites/1/login-credentials", "")
	require.Equal(t, 200, read.Code)
	require.Equal(t, "no-store", read.Header().Get("Cache-Control"))
	require.Contains(t, read.Body.String(), `"username":"admin-canary","password":"password-canary","version":2`)
	listed := request("GET", "/sites", "")
	require.NotContains(t, listed.Body.String(), "password-canary")
	require.NotContains(t, listed.Body.String(), store.site.LoginCipher)
	stale := request("PUT", "/sites/1", body)
	require.Equal(t, 409, stale.Code)
	require.NotContains(t, stale.Body.String(), "password-canary")
	for _, path := range []string{"/sites/0/login-credentials", "/sites/2/login-credentials"} {
		failed := request("GET", path, "")
		require.GreaterOrEqual(t, failed.Code, 400)
		require.Equal(t, "no-store", failed.Header().Get("Cache-Control"))
		require.NotContains(t, failed.Body.String(), "password-canary")
	}
	store.site.LoginCipher = "unreadable"
	failed := request("GET", "/sites/1/login-credentials", "")
	require.Equal(t, 503, failed.Code)
	require.Equal(t, "no-store", failed.Header().Get("Cache-Control"))
}
