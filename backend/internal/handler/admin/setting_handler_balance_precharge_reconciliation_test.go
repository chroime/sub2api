package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type reviewAdminRepository struct {
	service.BalancePrechargeReconciliationRepository
	command       *service.BalancePrechargeResolutionCommand
	err           error
	status        string
	limit, offset int
}

func (r *reviewAdminRepository) ListBalancePrechargeReviews(_ context.Context, status string, limit, offset int) ([]*service.BalancePrechargeReview, int64, error) {
	r.status, r.limit, r.offset = status, limit, offset
	return nil, 0, r.err
}
func (r *reviewAdminRepository) ResolveBalancePrechargeReview(_ context.Context, cmd *service.BalancePrechargeResolutionCommand) (*service.BalancePrechargeReview, error) {
	r.command = cmd
	if r.err != nil {
		return nil, r.err
	}
	return &service.BalancePrechargeReview{ID: cmd.PrechargeID, UserID: 1, Status: "resolved", Resolution: cmd.Action, Note: cmd.Note}, nil
}

func reviewAdminRequest(t *testing.T, repo *reviewAdminRepository, actor int64, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(nil, nil, nil, nil, nil, nil, nil)
	h.SetBalancePrechargeReconciliationService(service.NewBalancePrechargeReconciliationService(repo, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if actor > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
		}
		c.Next()
	})
	router.GET("/reviews", h.ListBalancePrechargeReviews)
	router.POST("/reviews/:id/resolve", h.ResolveBalancePrechargeReview)
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestBalancePrechargeReviewAdminUsesAuthenticatedActor(t *testing.T) {
	r := &reviewAdminRepository{}
	w := reviewAdminRequest(t, r, 77, "POST", "/reviews/"+uuid.NewString()+"/resolve", `{"action":"charge","actual_cost":0.01234567,"note":" invoice checked ","actor_id":999}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int64(77), r.command.ActorID)
	require.Equal(t, .01234567, r.command.ActualCost)
	require.Equal(t, "invoice checked", r.command.Note)
	require.Contains(t, w.Body.String(), `"status":"resolved"`)
}

func TestBalancePrechargeReviewAdminRejectsIncompleteAndInvalidResolutions(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"action":"release","note":"checked"}`, `{"action":"release","actual_cost":null,"note":"checked"}`, `{"action":"release","actual_cost":1,"note":"checked"}`, `{"action":"charge","actual_cost":0,"note":"checked"}`, `{"action":"charge","actual_cost":0.01,"note":" "}`} {
		r := &reviewAdminRepository{}
		w := reviewAdminRequest(t, r, 77, "POST", "/reviews/"+uuid.NewString()+"/resolve", body)
		require.Equal(t, http.StatusBadRequest, w.Code, body+": "+w.Body.String())
		require.Nil(t, r.command)
	}
	r := &reviewAdminRepository{}
	w := reviewAdminRequest(t, r, 0, "POST", "/reviews/"+uuid.NewString()+"/resolve", `{"action":"release","actual_cost":0,"note":"checked"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Nil(t, r.command)
}

func TestBalancePrechargeReviewAdminPropagatesConflict(t *testing.T) {
	r := &reviewAdminRepository{err: service.ErrBalancePrechargeReviewConflict}
	w := reviewAdminRequest(t, r, 77, "POST", "/reviews/"+uuid.NewString()+"/resolve", `{"action":"release","actual_cost":0,"note":"checked"}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}

func TestBalancePrechargeReviewAdminListValidatesPagination(t *testing.T) {
	r := &reviewAdminRepository{}
	w := reviewAdminRequest(t, r, 77, "GET", "/reviews?status=settled&limit=10&offset=20", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "settled", r.status)
	require.Equal(t, 10, r.limit)
	require.Equal(t, 20, r.offset)
	require.Contains(t, w.Body.String(), `"items":[]`)
	for _, query := range []string{"limit=0", "limit=201", "limit=x", "offset=-1", "offset=99999999999999999999999", "status=unknown"} {
		w = reviewAdminRequest(t, r, 77, "GET", "/reviews?"+query, "")
		require.Equal(t, http.StatusBadRequest, w.Code, query+": "+w.Body.String())
	}
}
