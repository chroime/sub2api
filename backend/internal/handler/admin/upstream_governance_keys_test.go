package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type governanceKeyHandlerStore struct {
	gov.Store
	site gov.Site
	key  gov.ManagedKey
}

func (s *governanceKeyHandlerStore) GetSite(context.Context, int64) (*gov.Site, error) {
	return &s.site, nil
}
func (s *governanceKeyHandlerStore) LockSite(context.Context, int64) (func(), bool, error) {
	return func() {}, true, nil
}
func (s *governanceKeyHandlerStore) ListManagedKeys(context.Context, int64) ([]gov.ManagedKey, error) {
	if s.key.ID == 0 {
		return []gov.ManagedKey{}, nil
	}
	return []gov.ManagedKey{s.key}, nil
}
func (s *governanceKeyHandlerStore) GetManagedKey(_ context.Context, siteID, keyID int64) (*gov.ManagedKey, error) {
	if siteID != s.key.SiteID || keyID != s.key.ID {
		return nil, gov.ErrNotFound
	}
	return &s.key, nil
}

func (s *governanceKeyHandlerStore) LatestSnapshot(context.Context, int64) (*gov.Snapshot, error) {
	return &gov.Snapshot{ID: 3, SiteID: 1, SiteVersion: 1, Catalog: gov.Catalog{Groups: []gov.RemoteGroup{{ID: "8", Platform: "openai"}}}}, nil
}
func (s *governanceKeyHandlerStore) ListBindings(context.Context, int64) ([]gov.Binding, error) {
	return nil, nil
}
func (s *governanceKeyHandlerStore) SaveManagedKey(_ context.Context, key *gov.ManagedKey) error {
	key.ID = 2
	key.HasKey = key.KeyCipher != ""
	s.key = *key
	return nil
}

type governanceKeyHandlerConnector struct{ gov.Connector }

func (*governanceKeyHandlerConnector) EnsureKey(context.Context, gov.Site, gov.Session, gov.RemoteGroup, string) (gov.RemoteKey, error) {
	return gov.RemoteKey{ID: "9", Key: "inference-key-canary"}, nil
}

func TestGovernanceKeyRevealIsExplicitAndMetadataHidesCiphertext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	encryptor, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	secret, err := json.Marshal(gov.RemoteKey{ID: "9", Key: "inference-key-canary"})
	require.NoError(t, err)
	ciphertext, err := encryptor.Encrypt(string(secret))
	require.NoError(t, err)
	store := &governanceKeyHandlerStore{site: gov.Site{ID: 1, Version: 1}, key: gov.ManagedKey{ID: 2, SiteID: 1, RemoteGroupID: "8", Platform: "openai", RemoteKeyID: "9", OwnerUserID: 5, KeyCipher: ciphertext, HasKey: true}}
	handler := NewUpstreamGovernanceHandler(gov.NewService(store, &governanceKeyHandlerConnector{}, nil, encryptor, true))
	r := gin.New()
	r.GET("/sites/:id/keys", handler.Keys)
	r.POST("/sites/:id/keys", handler.CreateKeys)
	r.POST("/sites/:id/keys/:key_id/reveal", handler.RevealKey)
	listed := httptest.NewRecorder()
	r.ServeHTTP(listed, httptest.NewRequest("GET", "/sites/1/keys", nil))
	require.Equal(t, 200, listed.Code)
	require.Contains(t, listed.Body.String(), `"remote_key_id":"9"`)
	require.NotContains(t, listed.Body.String(), "inference-key-canary")
	require.NotContains(t, listed.Body.String(), ciphertext)
	require.NotContains(t, listed.Body.String(), "owner_user_id")
	revealed := httptest.NewRecorder()
	r.ServeHTTP(revealed, httptest.NewRequest("POST", "/sites/1/keys/2/reveal", strings.NewReader(`{}`)))
	require.Equal(t, 200, revealed.Code)
	require.Equal(t, "no-store", revealed.Header().Get("Cache-Control"))
	require.Contains(t, revealed.Body.String(), `"key":"inference-key-canary"`)
	require.NotContains(t, revealed.Body.String(), ciphertext)
	otherSite := httptest.NewRecorder()
	r.ServeHTTP(otherSite, httptest.NewRequest("POST", "/sites/3/keys/2/reveal", strings.NewReader(`{}`)))
	require.Equal(t, 404, otherSite.Code)
	require.Equal(t, "no-store", otherSite.Header().Get("Cache-Control"))
	require.NotContains(t, otherSite.Body.String(), "inference-key-canary")
	store.key = gov.ManagedKey{}
	session, err := json.Marshal(gov.Session{AccessToken: "session-canary", UserID: 5})
	require.NoError(t, err)
	store.site.SessionCipher, err = encryptor.Encrypt(string(session))
	require.NoError(t, err)
	created := httptest.NewRecorder()
	r.ServeHTTP(created, httptest.NewRequest("POST", "/sites/1/keys", strings.NewReader(`{"snapshot_id":3,"selections":[{"remote_group_id":"8","platform":"openai"}]}`)))
	require.Equal(t, 200, created.Code)
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
	require.Contains(t, created.Body.String(), `"status":"created"`)
	require.Contains(t, created.Body.String(), `"key":"inference-key-canary"`)
	require.NotContains(t, created.Body.String(), store.key.KeyCipher)
	require.NotContains(t, created.Body.String(), "session-canary")
}

func TestGovernanceKeyMutationsRejectUnknownBodiesBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewUpstreamGovernanceHandler(nil)
	r := gin.New()
	r.POST("/sites/:id/keys", handler.CreateKeys)
	r.POST("/sites/:id/keys/:key_id/reveal", handler.RevealKey)
	for _, tc := range []struct{ path, body string }{
		{"/sites/1/keys", `{"snapshot_id":1,"selections":[],"owner_user_id":6}`},
		{"/sites/1/keys", `{"snapshot_id":1,"selections":[{"remote_group_id":"8","platform":"openai","key":"injected"}]}`},
		{"/sites/1/keys", `{} {}`},
		{"/sites/1/keys/2/reveal", `{"key":"injected"}`},
		{"/sites/1/keys/2/reveal", `null`},
		{"/sites/1/keys/2/reveal", `{} {}`},
		{"/sites/1/keys/02/reveal", `{}`},
		{"/sites/0/keys", `{}`},
	} {
		t.Run(tc.path+tc.body, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
			require.Equal(t, 400, w.Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.NotContains(t, w.Body.String(), "injected")
		})
	}
}
