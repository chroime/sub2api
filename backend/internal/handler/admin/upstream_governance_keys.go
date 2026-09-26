package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func governanceNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func (h *UpstreamGovernanceHandler) Keys(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	keys, err := h.svc.Keys(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, keys)
	}
}

func (h *UpstreamGovernanceHandler) CreateKeys(c *gin.Context) {
	governanceNoStore(c)
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input gov.CreateKeysInput
	if !governanceBody(c, &input) {
		return
	}
	result, err := h.svc.CreateKeys(c.Request.Context(), id, input)
	if !governanceError(c, err) {
		response.Success(c, result)
	}
}

func (h *UpstreamGovernanceHandler) RevealKey(c *gin.Context) {
	governanceNoStore(c)
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	keyID, ok := governanceID(c, "key_id")
	if !ok {
		return
	}
	var input *struct{}
	if !governanceBody(c, &input) {
		return
	}
	if input == nil {
		governanceError(c, gov.ErrInvalid)
		return
	}
	result, err := h.svc.RevealKey(c.Request.Context(), id, keyID)
	if !governanceError(c, err) {
		response.Success(c, result)
	}
}
