package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type userBulkEmailService interface {
	Send(context.Context, service.UserBulkEmailInput) (*service.UserBulkEmailResult, error)
	Preview(context.Context, service.UserBulkEmailPreviewInput) (service.UserBulkEmailPreviewResult, error)
}

func (h *UserHandler) SetBulkEmailService(svc *service.UserBulkEmailService) {
	h.bulkEmailService = svc
}

func decodeBulkEmailRequest(c *gin.Context, input any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		response.BadRequest(c, "Invalid bulk email request")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Invalid bulk email request")
		return false
	}
	return true
}

// BulkEmail waits for every attempted SMTP delivery and returns its result.
// POST /api/v1/admin/users/bulk-email
func (h *UserHandler) BulkEmail(c *gin.Context) {
	var input service.UserBulkEmailInput
	if !decodeBulkEmailRequest(c, &input) {
		return
	}
	if h.bulkEmailService == nil {
		response.InternalError(c, "Bulk email service is unavailable")
		return
	}
	result, err := h.bulkEmailService.Send(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// PreviewBulkEmail renders the selected user's live variables without sending.
// When user_id is omitted, the authenticated administrator is the preview user.
// POST /api/v1/admin/users/bulk-email/preview
func (h *UserHandler) PreviewBulkEmail(c *gin.Context) {
	var input service.UserBulkEmailPreviewInput
	if !decodeBulkEmailRequest(c, &input) {
		return
	}
	if input.UserID == 0 {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok {
			response.Unauthorized(c, "Authentication required")
			return
		}
		input.UserID = subject.UserID
	}
	if h.bulkEmailService == nil {
		response.InternalError(c, "Bulk email service is unavailable")
		return
	}
	preview, err := h.bulkEmailService.Preview(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, preview)
}
