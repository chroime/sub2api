package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ProxyHandler handles admin proxy management
type ProxyHandler struct {
	adminService  service.AdminService
	batchCreateMu sync.Mutex
}

// NewProxyHandler creates a new admin proxy handler
func NewProxyHandler(adminService service.AdminService) *ProxyHandler {
	return &ProxyHandler{
		adminService: adminService,
	}
}

// CreateProxyRequest represents create proxy request
type CreateProxyRequest struct {
	Name           string `json:"name" binding:"required"`
	Protocol       string `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host           string `json:"host" binding:"required"`
	Port           int    `json:"port" binding:"required,min=1,max=65535"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	ExpiresAt      *int64 `json:"expires_at"`
	FallbackMode   string `json:"fallback_mode" binding:"omitempty,oneof=none proxy direct"`
	BackupProxyID  *int64 `json:"backup_proxy_id"`
	ExpiryWarnDays int    `json:"expiry_warn_days" binding:"omitempty,min=0"`
}

// UpdateProxyRequest represents update proxy request
type UpdateProxyRequest struct {
	Name           string                 `json:"name"`
	Protocol       string                 `json:"protocol" binding:"omitempty,oneof=http https socks5 socks5h"`
	Host           string                 `json:"host"`
	Port           int                    `json:"port" binding:"omitempty,min=1,max=65535"`
	Username       *string                `json:"username"`
	Password       *string                `json:"password"`
	Status         string                 `json:"status" binding:"omitempty,oneof=active inactive"`
	ExpiresAt      dto.NullableInt64Field `json:"expires_at"`
	FallbackMode   string                 `json:"fallback_mode" binding:"omitempty,oneof=none proxy direct"`
	BackupProxyID  dto.NullableInt64Field `json:"backup_proxy_id"`
	ExpiryWarnDays *int                   `json:"expiry_warn_days" binding:"omitempty,min=0"`
}

// List handles listing all proxies with pagination
// GET /api/v1/admin/proxies
func (h *ProxyHandler) List(c *gin.Context) {
	if h.scopedAdminProxyRead(c) {
		return
	}
	page, pageSize := response.ParsePagination(c)
	protocol := c.Query("protocol")
	status := c.Query("status")
	search := c.Query("search")
	sortBy := c.DefaultQuery("sort_by", "id")
	sortOrder := c.DefaultQuery("sort_order", "desc")
	// 标准化和验证 search 参数
	search = strings.TrimSpace(search)
	if len(search) > 100 {
		search = search[:100]
	}

	proxies, total, err := h.adminService.ListProxiesWithAccountCount(c.Request.Context(), page, pageSize, protocol, status, search, sortBy, sortOrder)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminProxyWithAccountCount, 0, len(proxies))
	for i := range proxies {
		out = append(out, *dto.ProxyWithAccountCountFromServiceAdmin(&proxies[i]))
	}
	response.Paginated(c, out, total, page, pageSize)
}

// GetAll handles getting all active proxies without pagination
// GET /api/v1/admin/proxies/all
// Optional query param: with_count=true to include account count per proxy
func (h *ProxyHandler) GetAll(c *gin.Context) {
	if h.scopedAdminProxyRead(c) {
		return
	}
	withCount := c.Query("with_count") == "true"

	if withCount {
		proxies, err := h.adminService.GetAllProxiesWithAccountCount(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		out := make([]dto.AdminProxyWithAccountCount, 0, len(proxies))
		for i := range proxies {
			out = append(out, *dto.ProxyWithAccountCountFromServiceAdmin(&proxies[i]))
		}
		response.Success(c, out)
		return
	}

	proxies, err := h.adminService.GetAllProxies(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminProxy, 0, len(proxies))
	for i := range proxies {
		out = append(out, *dto.ProxyFromServiceAdmin(&proxies[i]))
	}
	response.Success(c, out)
}

// GetByID handles getting a proxy by ID
// GET /api/v1/admin/proxies/:id
func (h *ProxyHandler) GetByID(c *gin.Context) {
	if h.scopedAdminProxyRead(c) {
		return
	}
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	proxy, err := h.adminService.GetProxy(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ProxyFromServiceAdmin(proxy))
}

// Create handles creating a new proxy
// POST /api/v1/admin/proxies
func (h *ProxyHandler) Create(c *gin.Context) {
	var req CreateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	executeAdminIdempotentJSON(c, "admin.proxies.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		var expiresAt *time.Time
		if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
			t := time.Unix(*req.ExpiresAt, 0).UTC()
			expiresAt = &t
		}
		proxy, err := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
			Name:           strings.TrimSpace(req.Name),
			Protocol:       strings.TrimSpace(req.Protocol),
			Host:           strings.TrimSpace(req.Host),
			Port:           req.Port,
			Username:       strings.TrimSpace(req.Username),
			Password:       strings.TrimSpace(req.Password),
			ExpiresAt:      expiresAt,
			FallbackMode:   strings.TrimSpace(req.FallbackMode),
			BackupProxyID:  req.BackupProxyID,
			ExpiryWarnDays: req.ExpiryWarnDays,
		})
		if err != nil {
			return nil, err
		}
		return dto.ProxyFromServiceAdmin(proxy), nil
	})
}

// Update handles updating a proxy
// PUT /api/v1/admin/proxies/:id
func (h *ProxyHandler) Update(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	var req UpdateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt.Value != nil && *req.ExpiresAt.Value > 0 {
		t := time.Unix(*req.ExpiresAt.Value, 0).UTC()
		expiresAt = &t
	}
	if req.Username != nil {
		*req.Username = strings.TrimSpace(*req.Username)
	}
	if req.Password != nil {
		*req.Password = strings.TrimSpace(*req.Password)
	}
	proxy, err := h.adminService.UpdateProxy(c.Request.Context(), proxyID, &service.UpdateProxyInput{
		Name:           strings.TrimSpace(req.Name),
		Protocol:       strings.TrimSpace(req.Protocol),
		Host:           strings.TrimSpace(req.Host),
		Port:           req.Port,
		Username:       req.Username,
		Password:       req.Password,
		Status:         strings.TrimSpace(req.Status),
		ExpiresAt:      expiresAt,
		ClearExpiresAt: req.ExpiresAt.Set && expiresAt == nil,
		FallbackMode:   strings.TrimSpace(req.FallbackMode),
		BackupProxyID:  req.BackupProxyID.Value,
		ClearBackupID:  req.BackupProxyID.Set && req.BackupProxyID.Value == nil,
		ExpiryWarnDays: req.ExpiryWarnDays,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ProxyFromServiceAdmin(proxy))
}

// Delete handles deleting a proxy
// DELETE /api/v1/admin/proxies/:id
func (h *ProxyHandler) Delete(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	err = h.adminService.DeleteProxy(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Proxy deleted successfully"})
}

// BatchDelete handles batch deleting proxies
// POST /api/v1/admin/proxies/batch-delete
func (h *ProxyHandler) BatchDelete(c *gin.Context) {
	type BatchDeleteRequest struct {
		IDs []int64 `json:"ids" binding:"required,min=1"`
	}

	var req BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.adminService.BatchDeleteProxies(c.Request.Context(), req.IDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// Test handles testing proxy connectivity
// POST /api/v1/admin/proxies/:id/test
func (h *ProxyHandler) Test(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	result, err := h.adminService.TestProxy(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// CheckQuality handles checking proxy quality across common AI targets.
// POST /api/v1/admin/proxies/:id/quality-check
func (h *ProxyHandler) CheckQuality(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	result, err := h.adminService.CheckProxyQuality(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// GetStats handles getting proxy statistics
// GET /api/v1/admin/proxies/:id/stats
func (h *ProxyHandler) GetStats(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	// Return mock data for now
	_ = proxyID
	response.Success(c, gin.H{
		"total_accounts":  0,
		"active_accounts": 0,
		"total_requests":  0,
		"success_rate":    100.0,
		"average_latency": 0,
	})
}

// GetProxyAccounts handles getting accounts using a proxy
// GET /api/v1/admin/proxies/:id/accounts
func (h *ProxyHandler) GetProxyAccounts(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	accounts, err := h.adminService.GetProxyAccounts(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.ProxyAccountSummary, 0, len(accounts))
	for i := range accounts {
		out = append(out, *dto.ProxyAccountSummaryFromService(&accounts[i]))
	}
	response.Success(c, out)
}

// BatchCreateProxyItem represents a single proxy in batch create request
type BatchCreateProxyItem struct {
	Name     string `json:"name" binding:"required,max=100"`
	Protocol string `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required,min=1,max=65535"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// BatchCreateRequest represents batch create proxies request
type BatchCreateRequest struct {
	Proxies []BatchCreateProxyItem `json:"proxies" binding:"required,min=1,dive"`
}

// BatchCreate handles batch creating proxies
// POST /api/v1/admin/proxies/batch
func (h *ProxyHandler) BatchCreate(c *gin.Context) {
	var req BatchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Validate every name before creating any proxy, including later rows.
	names := make(map[string]int, len(req.Proxies))
	for i := range req.Proxies {
		item := &req.Proxies[i]
		item.Name = strings.TrimSpace(item.Name)
		item.Host = strings.TrimSpace(item.Host)
		item.Username = strings.TrimSpace(item.Username)
		item.Password = strings.TrimSpace(item.Password)
		if item.Name == "" || strings.ContainsFunc(item.Name, unicode.IsControl) {
			response.BadRequest(c, fmt.Sprintf("Proxy #%d name must not be blank or contain control characters", i+1))
			return
		}
		key := strings.ToLower(item.Name)
		if previous, exists := names[key]; exists {
			response.BadRequest(c, fmt.Sprintf("Proxy #%d name duplicates proxy #%d: %s", i+1, previous, item.Name))
			return
		}
		names[key] = i + 1
	}

	// Keep the name inventory and writes consistent across concurrent imports.
	h.batchCreateMu.Lock()
	defer h.batchCreateMu.Unlock()

	// Check all statuses, since the active-only list would allow reused names.
	const pageSize = 1000
	for page := 1; ; page++ {
		proxies, total, err := h.adminService.ListProxies(c.Request.Context(), page, pageSize, "", "", "", "id", "asc")
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		for _, proxy := range proxies {
			if row, exists := names[strings.ToLower(strings.TrimSpace(proxy.Name))]; exists {
				item := req.Proxies[row-1]
				if proxy.Host == item.Host && proxy.Port == item.Port && proxy.Username == item.Username && proxy.Password == item.Password {
					// Retrying a partially successful import skips this endpoint below.
					continue
				}
				response.BadRequest(c, fmt.Sprintf("Proxy #%d name already exists: %s", row, req.Proxies[row-1].Name))
				return
			}
		}
		if len(proxies) == 0 || int64(page)*pageSize >= total {
			break
		}
	}

	created := 0
	skipped := 0
	type endpointIdentity struct {
		host, username, password string
		port                     int
	}
	seenEndpoints := make(map[endpointIdentity]struct{}, len(req.Proxies))

	for _, item := range req.Proxies {
		// Trim all string fields
		host := strings.TrimSpace(item.Host)
		protocol := strings.TrimSpace(item.Protocol)
		username := strings.TrimSpace(item.Username)
		password := strings.TrimSpace(item.Password)
		identity := endpointIdentity{host, username, password, item.Port}
		if _, seen := seenEndpoints[identity]; seen {
			skipped++
			continue
		}

		// Check for duplicates (same host, port, username, password)
		exists, err := h.adminService.CheckProxyExists(c.Request.Context(), host, item.Port, username, password)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		if exists {
			seenEndpoints[identity] = struct{}{}
			skipped++
			continue
		}

		// Preserve the name confirmed in the import preview.
		_, err = h.adminService.CreateProxy(c.Request.Context(), &service.CreateProxyInput{
			Name:     item.Name,
			Protocol: protocol,
			Host:     host,
			Port:     item.Port,
			Username: username,
			Password: password,
		})
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		seenEndpoints[identity] = struct{}{}
		created++
	}

	response.Success(c, gin.H{
		"created": created,
		"skipped": skipped,
	})
}
