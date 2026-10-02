package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) Workbench(c *gin.Context) {
	page, size, ok := governancePage(c)
	if !ok {
		return
	}
	var siteID int64
	if raw, present := c.GetQuery("site_id"); present {
		var err error
		siteID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || siteID <= 0 || strconv.FormatInt(siteID, 10) != raw {
			governanceError(c, gov.ErrInvalid)
			return
		}
	}
	value, err := h.svc.Workbench(c.Request.Context(), gov.OperationsQuery{SiteID: siteID, Page: page, PageSize: size})
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) Timeline(c *gin.Context) {
	siteID, ok := governanceID(c, "id")
	if !ok {
		return
	}
	page, size, ok := governancePage(c)
	if !ok {
		return
	}
	kind := c.DefaultQuery("kind", "all")
	value, err := h.svc.Timeline(c.Request.Context(), gov.OperationsQuery{SiteID: siteID, Page: page, PageSize: size, Kind: kind})
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}
