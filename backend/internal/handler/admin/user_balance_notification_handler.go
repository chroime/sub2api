package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type balanceNotificationReader interface {
	List(context.Context, balancenotice.Query) (*balancenotice.Page, error)
	Get(context.Context, int64) (*balancenotice.Record, error)
}

func (h *UpstreamGovernanceHandler) SetBalanceNotificationRecords(records *balancenotice.Service) {
	h.balanceNotifications = records
}

func (h *UpstreamGovernanceHandler) ListBalanceNotifications(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	page, size, ok := governancePage(c)
	if !ok {
		return
	}
	q := balancenotice.Query{Page: page, PageSize: size, Status: c.Query("status"), TriggerType: c.Query("trigger_type"), UserEmail: c.Query("user_email"), Recipient: c.Query("recipient")}
	if raw, exists := c.GetQuery("user_id"); exists {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
			response.BadRequest(c, "Invalid user ID")
			return
		}
		q.UserID = id
	}
	for key, target := range map[string]*time.Time{"from": &q.From, "to": &q.To} {
		if raw, exists := c.GetQuery(key); exists {
			value, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				response.BadRequest(c, "Invalid notification time range")
				return
			}
			*target = value
		}
	}
	q, err := q.Normalize(time.Now().UTC())
	if err != nil {
		response.BadRequest(c, "Invalid balance notification filters")
		return
	}
	if h.balanceNotifications == nil {
		response.Error(c, http.StatusServiceUnavailable, "Balance notification records unavailable")
		return
	}
	result, err := h.balanceNotifications.List(c.Request.Context(), q)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *UpstreamGovernanceHandler) BalanceNotification(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	if h.balanceNotifications == nil {
		response.Error(c, http.StatusServiceUnavailable, "Balance notification records unavailable")
		return
	}
	result, err := h.balanceNotifications.Get(c.Request.Context(), id)
	if errors.Is(err, balancenotice.ErrNotFound) {
		response.NotFound(c, "Balance notification record not found")
		return
	}
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
