package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bulkEmailHandlerServiceStub struct {
	sends    []service.UserBulkEmailInput
	previews []service.UserBulkEmailPreviewInput
	err      error
}

func (s *bulkEmailHandlerServiceStub) Send(_ context.Context, input service.UserBulkEmailInput) (*service.UserBulkEmailResult, error) {
	s.sends = append(s.sends, input)
	return &service.UserBulkEmailResult{Total: 2, Sent: 1, Failed: 1, Items: []service.UserBulkEmailItem{{UserID: 1, Email: "one@example.com", Status: "sent"}, {UserID: 2, Email: "two@example.com", Status: "failed", Error: "email_delivery_failed"}}}, s.err
}
func (s *bulkEmailHandlerServiceStub) Preview(_ context.Context, input service.UserBulkEmailPreviewInput) (service.UserBulkEmailPreviewResult, error) {
	s.previews = append(s.previews, input)
	result := service.UserBulkEmailPreviewResult{NotificationEmailPreview: service.NotificationEmailPreview{Subject: "Current health", HTML: "<p>Current live groups</p>"}}
	if input.Links != nil {
		result.Links = *input.Links
	}
	return result, s.err
}

func bulkEmailHandlerRouter(svc userBulkEmailService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	})
	h := &UserHandler{bulkEmailService: svc}
	r.POST("/bulk-email", h.BulkEmail)
	r.POST("/bulk-email/preview", h.PreviewBulkEmail)
	return r
}

func requestBulkEmail(r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestUserHandlerBulkEmailReturnsPerUserResult(t *testing.T) {
	svc := &bulkEmailHandlerServiceStub{}
	w := requestBulkEmail(bulkEmailHandlerRouter(svc), "/bulk-email", `{"user_ids":[1,2],"event":"system.health_snapshot","locale":"zh"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, svc.sends, 1)
	require.Equal(t, []int64{1, 2}, svc.sends[0].UserIDs)
	var response struct {
		Data service.UserBulkEmailResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, 1, response.Data.Sent)
	require.Equal(t, 1, response.Data.Failed)
	require.Equal(t, "failed", response.Data.Items[1].Status)
}

func TestUserHandlerBulkEmailPreviewDefaultsToAuthenticatedUser(t *testing.T) {
	svc := &bulkEmailHandlerServiceStub{}
	r := bulkEmailHandlerRouter(svc)
	w := requestBulkEmail(r, "/bulk-email/preview", `{"event":"system.health_snapshot","locale":"en"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, svc.previews, 1)
	require.Equal(t, int64(42), svc.previews[0].UserID)
	require.Empty(t, svc.sends)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	w = requestBulkEmail(r, "/bulk-email/preview", `{"user_id":7,"event":"balance.low"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(7), svc.previews[1].UserID)
}

func TestUserHandlerBulkEmailAcceptsLinksAndPreviewReturnsThem(t *testing.T) {
	svc := &bulkEmailHandlerServiceStub{}
	r := bulkEmailHandlerRouter(svc)
	w := requestBulkEmail(r, "/bulk-email", `{"user_ids":[1],"event":"system.health_snapshot","links":{"platform_url":"https://site.example","docs_url":"https://docs.example","model_plaza_url":"https://models.example","canvas_url":"https://canvas.example"}}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, svc.sends, 1)
	require.NotNil(t, svc.sends[0].Links)
	require.Equal(t, "https://site.example", svc.sends[0].Links.PlatformURL)
	require.Equal(t, "https://docs.example", svc.sends[0].Links.DocsURL)
	require.Equal(t, "https://models.example", svc.sends[0].Links.ModelPlazaURL)
	require.Equal(t, "https://canvas.example", svc.sends[0].Links.CanvasURL)
	w = requestBulkEmail(r, "/bulk-email/preview", `{"user_id":1,"event":"system.health_snapshot","links":{"canvas_url":"https://canvas.example"}}`)
	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Contains(t, response.Data, "subject")
	require.Contains(t, response.Data, "html")
	require.Contains(t, response.Data, "links")
	links, ok := response.Data["links"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://canvas.example", links["canvas_url"])
}

func TestUserHandlerBulkEmailRejectsRecipientAndTemplateInjection(t *testing.T) {
	svc := &bulkEmailHandlerServiceStub{}
	r := bulkEmailHandlerRouter(svc)
	for _, body := range []string{
		`{"user_ids":[1],"event":"system.health_snapshot","email":"attacker@example.com"}`,
		`{"user_ids":[1],"event":"system.health_snapshot","variables":{"verification_code":"123456"}}`,
		`{"user_ids":[1],"event":"system.health_snapshot"} {}`,
		`{`,
	} {
		w := requestBulkEmail(r, "/bulk-email", body)
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	require.Empty(t, svc.sends)
	svc.err = service.ErrUserBulkEmailInvalid
	w := requestBulkEmail(r, "/bulk-email", `{"user_ids":[1],"event":"auth.verify_code"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
