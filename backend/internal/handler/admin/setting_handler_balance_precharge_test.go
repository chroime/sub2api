package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type balancePrechargeAdminRepo struct {
	service.SettingRepository
	values   map[string]string
	readErr  error
	writeErr error
}

func (r *balancePrechargeAdminRepo) GetValue(_ context.Context, key string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *balancePrechargeAdminRepo) Set(_ context.Context, key, value string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

type balancePrechargeAdminGroupReader struct{}

func (balancePrechargeAdminGroupReader) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if id != 7 {
		return nil, service.ErrGroupNotFound
	}
	return &service.Group{ID: id}, nil
}

func balancePrechargeAdminHandler(repo *balancePrechargeAdminRepo) *SettingHandler {
	svc := service.NewSettingService(repo, nil)
	svc.SetDefaultSubscriptionGroupReader(balancePrechargeAdminGroupReader{})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
}

func balancePrechargeAdminRequest(h *SettingHandler, method, group, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if group != "" {
		c.Params = gin.Params{{Key: "id", Value: group}}
		if method == http.MethodPut {
			h.UpdateGroupBalancePrechargeSettings(c)
		} else {
			h.GetGroupBalancePrechargeSettings(c)
		}
	} else if method == http.MethodPut {
		h.UpdateBalancePrechargeSettings(c)
	} else {
		h.GetBalancePrechargeSettings(c)
	}
	return recorder
}

func TestBalancePrechargeAdminRequiresCompleteValidatedSettings(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"enabled":false}`, `{"enabled":null,"threshold":1,"amount":1}`, `{"enabled":true,"threshold":1,"amount":2}`, `{"enabled":true,"threshold":1,"amount":0.000000001}`} {
		t.Run(body, func(t *testing.T) {
			repo := &balancePrechargeAdminRepo{}
			recorder := balancePrechargeAdminRequest(balancePrechargeAdminHandler(repo), http.MethodPut, "", body)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Empty(t, repo.values)
		})
	}
}

func TestBalancePrechargeAdminGlobalAndGroupEffectiveValues(t *testing.T) {
	h := balancePrechargeAdminHandler(&balancePrechargeAdminRepo{})
	initial := balancePrechargeAdminRequest(h, http.MethodGet, "", "")
	require.Equal(t, http.StatusOK, initial.Code)
	require.Contains(t, initial.Body.String(), `"enabled":false`)
	global := balancePrechargeAdminRequest(h, http.MethodPut, "", `{"enabled":true,"threshold":10,"amount":1}`)
	require.Equal(t, http.StatusOK, global.Code)
	group := balancePrechargeAdminRequest(h, http.MethodGet, "7", "")
	require.Equal(t, http.StatusOK, group.Code)
	require.Contains(t, group.Body.String(), `"mode":"inherit"`)
	require.Contains(t, group.Body.String(), `"effective":{"enabled":true,"threshold":10,"amount":1}`)
	updated := balancePrechargeAdminRequest(h, http.MethodPut, "7", `{"mode":"custom","threshold":5,"amount":2}`)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Contains(t, updated.Body.String(), `"group_id":7`)
	require.Contains(t, updated.Body.String(), `"effective":{"enabled":true,"threshold":5,"amount":2}`)
	reset := balancePrechargeAdminRequest(h, http.MethodPut, "7", `{"mode":"inherit","threshold":0,"amount":0}`)
	require.Equal(t, http.StatusOK, reset.Code)
	require.Contains(t, reset.Body.String(), `"effective":{"enabled":true,"threshold":10,"amount":1}`)
}

func TestBalancePrechargeAdminRejectsUnknownGroupsAndStorageFailures(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, test := range []struct {
			id     string
			status int
		}{{"0", 400}, {"bad", 400}, {"404", 404}} {
			repo := &balancePrechargeAdminRepo{}
			response := balancePrechargeAdminRequest(balancePrechargeAdminHandler(repo), method, test.id, `{"mode":"inherit","threshold":0,"amount":0}`)
			require.Equal(t, test.status, response.Code)
			require.Empty(t, repo.values)
		}
	}
	repo := &balancePrechargeAdminRepo{readErr: errors.New("offline")}
	h := balancePrechargeAdminHandler(repo)
	require.Equal(t, 500, balancePrechargeAdminRequest(h, http.MethodGet, "", "").Code)
	require.Equal(t, 500, balancePrechargeAdminRequest(h, http.MethodGet, "7", "").Code)
	repo.readErr = nil
	repo.writeErr = errors.New("offline")
	require.Equal(t, 500, balancePrechargeAdminRequest(h, http.MethodPut, "", `{"enabled":false,"threshold":0,"amount":0}`).Code)
}
