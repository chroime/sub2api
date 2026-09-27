package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/upstreambrowser"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) AuthorizationStatus(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.AuthorizationStatus(c.Request.Context(), id)
	if governanceError(c, err) {
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"session": value, "browser": h.svc.BrowserAuthorizer().Availability()})
}

func (h *UpstreamGovernanceHandler) browserRequest(c *gin.Context, withJob bool) (*gov.BrowserAuthorizer, int64, int64, string, bool) {
	id, ok := governanceID(c, "id")
	if !ok {
		return nil, 0, 0, "", false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator identity is required")
		return nil, 0, 0, "", false
	}
	job := ""
	if withJob {
		job, ok = governanceModelUUID(c, c.Param("job_id"))
		if !ok {
			return nil, 0, 0, "", false
		}
	}
	var authorizer *gov.BrowserAuthorizer
	if h.svc != nil {
		authorizer = h.svc.BrowserAuthorizer()
	}
	if authorizer == nil {
		governanceError(c, gov.ErrBrowserUnavailable)
		return nil, 0, 0, "", false
	}
	c.Header("Cache-Control", "no-store")
	return authorizer, subject.UserID, id, job, true
}

func (h *UpstreamGovernanceHandler) StartBrowserAuthorization(c *gin.Context) {
	a, actor, id, _, ok := h.browserRequest(c, false)
	if !ok {
		return
	}
	var input gov.BrowserAuthorizationInput
	if !governanceBody(c, &input) {
		return
	}
	value, err := a.Start(c.Request.Context(), actor, id, input)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) BrowserAuthorization(c *gin.Context) {
	a, actor, id, job, ok := h.browserRequest(c, true)
	if !ok {
		return
	}
	value, err := a.Get(c.Request.Context(), actor, id, job)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) BrowserAuthorizationAction(c *gin.Context) {
	a, actor, id, job, ok := h.browserRequest(c, true)
	if !ok {
		return
	}
	var input upstreambrowser.Action
	if !governanceBody(c, &input) {
		return
	}
	if !governanceError(c, a.Action(c.Request.Context(), actor, id, job, input)) {
		response.Success(c, gin.H{"accepted": true})
	}
}

func (h *UpstreamGovernanceHandler) CompleteBrowserAuthorization(c *gin.Context) {
	a, actor, id, job, ok := h.browserRequest(c, true)
	if !ok {
		return
	}
	var input struct{}
	if !governanceBody(c, &input) {
		return
	}
	value, err := a.Complete(c.Request.Context(), actor, id, job)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) CancelBrowserAuthorization(c *gin.Context) {
	a, actor, id, job, ok := h.browserRequest(c, true)
	if !ok {
		return
	}
	if !governanceError(c, a.Cancel(c.Request.Context(), actor, id, job)) {
		response.Success(c, gin.H{"cancelled": true})
	}
}
