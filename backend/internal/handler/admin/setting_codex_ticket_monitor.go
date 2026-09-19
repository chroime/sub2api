package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codexTicketMonitorSource interface {
	GetOpenAICodexTicketMonitor() service.OpenAICodexTicketMonitorSnapshot
}

// SetCodexTicketMonitorService attaches the current instance's native monitor.
func (h *SettingHandler) SetCodexTicketMonitorService(source codexTicketMonitorSource) {
	h.codexTicketMonitor = source
}

// GetCodexTicketMonitor returns bounded, credential-free runtime diagnostics.
// Authorization is inherited from the administrator settings route group.
func (h *SettingHandler) GetCodexTicketMonitor(c *gin.Context) {
	if h.codexTicketMonitor == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex ticket monitor unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, h.codexTicketMonitor.GetOpenAICodexTicketMonitor())
}
