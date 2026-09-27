package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UpstreamGovernanceHandler struct{ svc *gov.Service }

func NewUpstreamGovernanceHandler(svc *gov.Service) *UpstreamGovernanceHandler {
	return &UpstreamGovernanceHandler{svc: svc}
}
func governanceError(c *gin.Context, e error) bool {
	if e == nil {
		return false
	}
	status := http.StatusBadGateway
	message := "Upstream governance operation failed"
	switch {
	case errors.Is(e, gov.ErrInvalid):
		status = 400
		message = "Invalid governance input"
	case errors.Is(e, gov.ErrModelBudget):
		response.ErrorWithDetails(c, http.StatusConflict, "Model monitoring daily request budget exhausted", "model_budget_exhausted", nil)
		return true
	case errors.Is(e, gov.ErrModelGroupGone):
		response.ErrorWithDetails(c, http.StatusConflict, "The upstream group is no longer visible", "model_group_gone", nil)
		return true
	case errors.Is(e, gov.ErrNotFound):
		status = 404
		message = "Governance resource not found"
	case errors.Is(e, gov.ErrConflict), errors.Is(e, gov.ErrBusy):
		status = 409
		message = "Governance resource changed or is busy"
	case errors.Is(e, gov.ErrSiteInUse):
		status = http.StatusConflict
		message = "This site still has active account bindings, pending imports, or managed keys. Disable automatic collection to retain these resources."
	case errors.Is(e, gov.ErrEncryption):
		status = 503
		message = "Persistent encryption key is required"
	case errors.Is(e, gov.ErrReauth):
		status = 409
		message = "Upstream authorization is required"
	case errors.Is(e, gov.ErrUnsupported):
		message = "Upstream contract is unsupported"
	}
	response.ErrorWithDetails(c, status, message, gov.ErrorCode(e), nil)
	return true
}
func governanceID(c *gin.Context, name string) (int64, bool) {
	raw := c.Param(name)
	id, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		governanceError(c, gov.ErrInvalid)
		return 0, false
	}
	return id, true
}
func governanceBody(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		governanceError(c, gov.ErrInvalid)
		return false
	}
	return true
}

type governanceSiteInput struct {
	Name            string `json:"name"`
	Platform        string `json:"platform"`
	BaseURL         string `json:"base_url"`
	ProxyID         *int64 `json:"proxy_id"`
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"interval_minutes"`
	Version         int64  `json:"version"`
}

func (i governanceSiteInput) site() gov.Site {
	return gov.Site{Name: i.Name, Platform: i.Platform, BaseURL: i.BaseURL, ProxyID: i.ProxyID, Enabled: i.Enabled, IntervalMinutes: i.IntervalMinutes, Version: i.Version}
}
func (h *UpstreamGovernanceHandler) List(c *gin.Context) {
	v, e := h.svc.ListSites(c.Request.Context())
	if governanceError(c, e) {
		return
	}
	if v == nil {
		v = []gov.Site{}
	}
	response.Success(c, v)
}
func (h *UpstreamGovernanceHandler) Create(c *gin.Context) {
	var in governanceSiteInput
	if !governanceBody(c, &in) {
		return
	}
	v, e := h.svc.CreateSite(c.Request.Context(), in.site())
	if !governanceError(c, e) {
		response.Created(c, v)
	}
}
func (h *UpstreamGovernanceHandler) Update(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var in struct {
		governanceSiteInput
		LoginCredentials *gov.LoginCredentials `json:"login_credentials"`
	}
	if !governanceBody(c, &in) {
		return
	}
	v, e := h.svc.UpdateSiteWithLogin(c.Request.Context(), id, in.site(), in.LoginCredentials)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}

func (h *UpstreamGovernanceHandler) LoginCredentials(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	v, e := h.svc.LoginCredentials(c.Request.Context(), id)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}
func (h *UpstreamGovernanceHandler) Delete(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	if !governanceError(c, h.svc.DeleteSite(c.Request.Context(), id)) {
		response.Success(c, gin.H{"deleted": true})
	}
}
func (h *UpstreamGovernanceHandler) Connect(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var in gov.LoginInput
	if !governanceBody(c, &in) {
		return
	}
	v, e := h.svc.Connect(c.Request.Context(), id, in)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}
func (h *UpstreamGovernanceHandler) Sync(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	v, e := h.svc.Sync(c.Request.Context(), id)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}
func (h *UpstreamGovernanceHandler) Catalog(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	v, e := h.svc.Catalog(c.Request.Context(), id)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}
func (h *UpstreamGovernanceHandler) Bindings(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	v, e := h.svc.Bindings(c.Request.Context(), id)
	if governanceError(c, e) {
		return
	}
	if v == nil {
		v = []gov.Binding{}
	}
	response.Success(c, v)
}
func (h *UpstreamGovernanceHandler) Preview(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Selections []gov.Selection `json:"selections"`
	}
	if !governanceBody(c, &in) {
		return
	}
	v, e := h.svc.Preview(c.Request.Context(), id, in.Selections)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}
func (h *UpstreamGovernanceHandler) Apply(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	preview := c.Param("preview_id")
	if _, e := uuid.Parse(preview); e != nil {
		governanceError(c, gov.ErrInvalid)
		return
	}
	v, e := h.svc.Apply(c.Request.Context(), id, preview)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}
func governancePage(c *gin.Context) (int, int, bool) {
	page, size := 1, 20
	for key, dst := range map[string]*int{"page": &page, "page_size": &size} {
		if raw, ok := c.GetQuery(key); ok {
			v, e := strconv.Atoi(raw)
			if e != nil || v < 1 {
				governanceError(c, gov.ErrInvalid)
				return 0, 0, false
			}
			*dst = v
		}
	}
	if size > 100 || page > 100000 {
		governanceError(c, gov.ErrInvalid)
		return 0, 0, false
	}
	return page, size, true
}
func (h *UpstreamGovernanceHandler) Events(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	page, size, ok := governancePage(c)
	if !ok {
		return
	}
	v, total, e := h.svc.Events(c.Request.Context(), id, page, size)
	if governanceError(c, e) {
		return
	}
	if v == nil {
		v = []gov.Event{}
	}
	response.Paginated(c, v, total, page, size)
}
func (h *UpstreamGovernanceHandler) Checks(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	page, size, ok := governancePage(c)
	if !ok {
		return
	}
	v, total, e := h.svc.Checks(c.Request.Context(), id, page, size)
	if governanceError(c, e) {
		return
	}
	if v == nil {
		v = []gov.Check{}
	}
	response.Paginated(c, v, total, page, size)
}
func (h *UpstreamGovernanceHandler) Acknowledge(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	event, ok := governanceID(c, "event_id")
	if !ok {
		return
	}
	if !governanceError(c, h.svc.Acknowledge(c.Request.Context(), id, event)) {
		response.Success(c, gin.H{"acknowledged": true})
	}
}
func (h *UpstreamGovernanceHandler) Probe(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	binding, ok := governanceID(c, "binding_id")
	if !ok {
		return
	}
	var in struct {
		Model string `json:"model"`
	}
	if !governanceBody(c, &in) {
		return
	}
	v, e := h.svc.Probe(c.Request.Context(), id, binding, in.Model)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}
func (h *UpstreamGovernanceHandler) Monitor(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	binding, ok := governanceID(c, "binding_id")
	if !ok {
		return
	}
	var in struct {
		Enabled         bool   `json:"enabled"`
		Model           string `json:"model"`
		IntervalMinutes int    `json:"interval_minutes"`
	}
	if !governanceBody(c, &in) {
		return
	}
	v, e := h.svc.ConfigureMonitor(c.Request.Context(), id, binding, in.Enabled, in.Model, in.IntervalMinutes)
	if !governanceError(c, e) {
		response.Success(c, v)
	}
}
