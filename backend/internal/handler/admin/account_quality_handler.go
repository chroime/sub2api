package admin

import (
	"encoding/json"
	"errors"
	aq "github.com/Wei-Shaw/sub2api/internal/accountquality"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
)

type AccountQualityHandler struct{ svc *aq.Service }

func NewAccountQualityHandler(svc *aq.Service) *AccountQualityHandler {
	return &AccountQualityHandler{svc}
}
func qualityError(c *gin.Context, e error) bool {
	if e == nil {
		return false
	}
	status, reason, message := http.StatusInternalServerError, "ACCOUNT_QUALITY_ERROR", "账号质量操作失败"
	switch {
	case errors.Is(e, aq.ErrInvalid):
		status, reason, message = 400, "INVALID_ACCOUNT_QUALITY_INPUT", e.Error()
	case errors.Is(e, aq.ErrNotFound):
		status, reason, message = 404, "ACCOUNT_QUALITY_NOT_FOUND", "账号质量记录不存在"
	case errors.Is(e, aq.ErrConflict):
		status, reason, message = 409, "ACCOUNT_QUALITY_VERSION_CONFLICT", "配置已变化，请刷新后重试"
	case errors.Is(e, aq.ErrBusy):
		status, reason, message = 409, "ACCOUNT_QUALITY_BUSY", "该账号已有排队或执行中的检测"
	case errors.Is(e, aq.ErrBudget):
		status, reason, message = 429, "ACCOUNT_QUALITY_BUDGET_PAUSED", "诊断预算已用尽"
	}
	response.ErrorWithDetails(c, status, message, reason, nil)
	return true
}
func qualityID(c *gin.Context) int64 {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id < 1 {
		qualityError(c, aq.ErrInvalid)
		return 0
	}
	return id
}
func qualityActor(c *gin.Context) int64 {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "管理员身份不可用")
		return 0
	}
	return subject.UserID
}
func qualityDecode(c *gin.Context, v any) bool {
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 256*1024))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(v); e != nil {
		qualityError(c, aq.ErrInvalid)
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		qualityError(c, aq.ErrInvalid)
		return false
	}
	return true
}
func (h *AccountQualityHandler) Overview(c *gin.Context) {
	v, e := h.svc.Overview(c.Request.Context())
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) Policies(c *gin.Context) {
	v, e := h.svc.Store.Policies(c.Request.Context())
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) SavePolicy(c *gin.Context) {
	actor := qualityActor(c)
	if actor == 0 {
		return
	}
	p := aq.DefaultPolicy()
	if !qualityDecode(c, &p) {
		return
	}
	if c.Param("id") != "" {
		id := qualityID(c)
		if id == 0 {
			return
		}
		p.ID = id
		if p.Version < 1 {
			qualityError(c, aq.ErrInvalid)
			return
		}
	} else {
		p.ID = 0
		p.Version = 0
	}
	supported := false
	for _, v := range h.svc.Capabilities {
		if v.Platform == p.Platform && v.APIMode == p.APIMode {
			supported = true
		}
	}
	if !supported {
		qualityError(c, aq.ErrInvalid)
		return
	}
	v, e := h.svc.Store.SavePolicy(c.Request.Context(), p, actor)
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) DeletePolicy(c *gin.Context) {
	actor := qualityActor(c)
	id := qualityID(c)
	if actor == 0 || id == 0 {
		return
	}
	version, e := strconv.ParseInt(c.Query("version"), 10, 64)
	if e != nil || version < 1 {
		qualityError(c, aq.ErrInvalid)
		return
	}
	if !qualityError(c, h.svc.Store.DeletePolicy(c.Request.Context(), id, version, actor)) {
		response.Success(c, map[string]bool{"deleted": true})
	}
}
func (h *AccountQualityHandler) Bind(c *gin.Context) {
	actor := qualityActor(c)
	if actor == 0 {
		return
	}
	var in aq.BindInput
	if !qualityDecode(c, &in) {
		return
	}
	v, e := h.svc.Store.Bind(c.Request.Context(), in, actor)
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) RunNow(c *gin.Context) {
	actor := qualityActor(c)
	id := qualityID(c)
	if actor == 0 || id == 0 {
		return
	}
	var in struct {
		RequestID string `json:"request_id"`
		Version   int64  `json:"version"`
	}
	if !qualityDecode(c, &in) {
		return
	}
	if in.Version < 1 {
		qualityError(c, aq.ErrInvalid)
		return
	}
	v, e := h.svc.Queue(c.Request.Context(), id, in.Version, actor, in.RequestID)
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) Control(c *gin.Context) {
	actor := qualityActor(c)
	id := qualityID(c)
	if actor == 0 || id == 0 {
		return
	}
	var in aq.ControlInput
	if !qualityDecode(c, &in) {
		return
	}
	v, e := h.svc.Store.Control(c.Request.Context(), id, in, actor)
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) Summaries(c *gin.Context) {
	var in struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if !qualityDecode(c, &in) {
		return
	}
	if len(in.AccountIDs) > 500 {
		qualityError(c, aq.ErrInvalid)
		return
	}
	for _, id := range in.AccountIDs {
		if id <= 0 {
			qualityError(c, aq.ErrInvalid)
			return
		}
	}
	out, e := h.svc.Summaries(c.Request.Context(), in.AccountIDs)
	if !qualityError(c, e) {
		response.Success(c, out)
	}
}
func (h *AccountQualityHandler) Detail(c *gin.Context) {
	id := qualityID(c)
	if id == 0 {
		return
	}
	v, e := h.svc.Detail(c.Request.Context(), id)
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) Runs(c *gin.Context) {
	account, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	binding, _ := strconv.ParseInt(c.Query("binding_id"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	v, e := h.svc.Store.Runs(c.Request.Context(), account, binding, limit)
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) Run(c *gin.Context) {
	id := c.Param("id")
	if len(id) > 100 {
		qualityError(c, aq.ErrInvalid)
		return
	}
	v, e := h.svc.Store.Run(c.Request.Context(), id)
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) Cancel(c *gin.Context) {
	actor := qualityActor(c)
	if actor == 0 {
		return
	}
	if !qualityError(c, h.svc.Cancel(c.Request.Context(), c.Param("id"), actor)) {
		response.Success(c, map[string]bool{"cancelled": true})
	}
}
func (h *AccountQualityHandler) Settings(c *gin.Context) {
	v, e := h.svc.Store.Settings(c.Request.Context())
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
func (h *AccountQualityHandler) SaveSettings(c *gin.Context) {
	actor := qualityActor(c)
	if actor == 0 {
		return
	}
	var in aq.Settings
	if !qualityDecode(c, &in) {
		return
	}
	v, e := h.svc.Store.SaveSettings(c.Request.Context(), in, actor)
	if !qualityError(c, e) {
		response.Success(c, v)
	}
}
