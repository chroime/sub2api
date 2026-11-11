package admin

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *PaymentHandler) GetBillEditCredentials(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	subject, authenticated := middleware.GetAuthSubjectFromContext(c)
	if !authenticated || subject.UserID <= 0 {
		response.Unauthorized(c, "authentication required")
		return
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "administrator access required")
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	credentials, err := h.paymentService.GetAlipayBillEditCredentials(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, credentials)
}

func (h *PaymentHandler) TestBillConnection(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.paymentService.TestAlipayBillConnection(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentHandler) SyncBillReceipts(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.paymentService.SyncAlipayBills(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentHandler) ListBillReceipts(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := h.paymentService.ListAlipayBillReceipts(c.Request.Context(), id, page, size, c.Query("status"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentHandler) ClaimBillReceipt(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "authentication required"})
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	receiptID, ok := parseIDParam(c, "receiptId")
	if !ok {
		return
	}
	var req struct {
		OrderID     int64  `json:"order_id" binding:"omitempty,gt=0"`
		EpayOrderID int64  `json:"epay_order_id" binding:"omitempty,gt=0"`
		Reason      string `json:"reason" binding:"required,max=500"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "provide a positive order ID and verification reason")
		return
	}
	if (req.OrderID > 0) == (req.EpayOrderID > 0) {
		response.BadRequest(c, "provide exactly one local or gateway order ID")
		return
	}
	actor := fmt.Sprintf("admin:%d", subject.UserID)
	var err error
	if req.EpayOrderID > 0 {
		err = h.paymentService.ClaimBuiltinEasyPayReceipt(c.Request.Context(), id, receiptID, req.EpayOrderID, actor, req.Reason)
	} else {
		err = h.paymentService.ClaimAlipayBillReceipt(c.Request.Context(), id, receiptID, req.OrderID, actor, req.Reason)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}
