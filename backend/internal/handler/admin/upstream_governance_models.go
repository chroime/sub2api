package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func governanceModelUUID(c *gin.Context, value string) (string, bool) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		governanceError(c, gov.ErrInvalid)
		return "", false
	}
	return value, true
}

func (h *UpstreamGovernanceHandler) ModelStats(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	days := 1
	if raw := c.Query("days"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || strconv.Itoa(value) != raw || (value != 1 && value != 7 && value != 30) {
			governanceError(c, gov.ErrInvalid)
			return
		}
		days = value
	}
	value, err := h.svc.ModelStats(c.Request.Context(), id, days)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ModelPolicies(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.ListModelPolicies(c.Request.Context(), id)
	if !governanceError(c, err) {
		if value == nil {
			value = []gov.ModelPolicy{}
		}
		response.Success(c, value)
	}
}

// LocalModelTargets returns only the current administrator's usable local
// gateway keys and group metadata. Credential material is never serialized.
func (h *UpstreamGovernanceHandler) LocalModelTargets(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator identity is required")
		return
	}
	value, err := h.svc.ListLocalModelTargets(c.Request.Context(), subject.UserID)
	if !governanceError(c, err) {
		if value == nil {
			value = []gov.LocalModelTarget{}
		}
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) SaveModelPolicy(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input struct {
		ID                int64               `json:"id"`
		Name              string              `json:"name"`
		Config            gov.ModelTestConfig `json:"config"`
		Enabled           bool                `json:"enabled"`
		IntervalMinutes   int                 `json:"interval_minutes"`
		DailyRequestLimit int                 `json:"daily_request_limit"`
		NotifyEnabled     bool                `json:"notify_enabled"`
		Recipients        []string            `json:"recipients"`
		FailureThreshold  int                 `json:"failure_threshold"`
		TakeOverLegacy    bool                `json:"take_over_legacy"`
		Version           int64               `json:"version"`
	}
	if !governanceBody(c, &input) {
		return
	}
	if input.ID < 0 || input.Version < 0 || (input.ID > 0 && input.Version == 0) {
		governanceError(c, gov.ErrInvalid)
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator identity is required")
		return
	}
	value, err := h.svc.SaveModelPolicy(c.Request.Context(), id, gov.ModelPolicy{
		ID: input.ID, Name: input.Name, Config: input.Config, Enabled: input.Enabled,
		IntervalMinutes: input.IntervalMinutes, DailyRequestLimit: input.DailyRequestLimit,
		NotifyEnabled: input.NotifyEnabled, Recipients: input.Recipients,
		FailureThreshold: input.FailureThreshold, TakeOverLegacy: input.TakeOverLegacy, Version: input.Version,
	}, subject.UserID)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) DeleteModelPolicy(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	policyID, ok := governanceID(c, "policy_id")
	if !ok {
		return
	}
	raw := c.Query("version")
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || version <= 0 || strconv.FormatInt(version, 10) != raw {
		governanceError(c, gov.ErrInvalid)
		return
	}
	if !governanceError(c, h.svc.DeleteModelPolicy(c.Request.Context(), id, policyID, version)) {
		response.Success(c, gin.H{"deleted": true})
	}
}

func (h *UpstreamGovernanceHandler) StartModelBatch(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input gov.ModelBatchInput
	if !governanceBody(c, &input) {
		return
	}
	if _, ok := governanceModelUUID(c, input.RequestID); !ok {
		return
	}
	if input.PolicyID < 0 {
		governanceError(c, gov.ErrInvalid)
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator identity is required")
		return
	}
	value, err := h.svc.StartModelBatch(c.Request.Context(), id, input, subject.UserID)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) CancelModelBatch(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	batch, ok := governanceModelUUID(c, c.Param("batch_id"))
	if !ok {
		return
	}
	var input struct{}
	if !governanceBody(c, &input) {
		return
	}
	if !governanceError(c, h.svc.CancelModelBatch(c.Request.Context(), id, batch)) {
		response.Success(c, gin.H{"cancelled": true})
	}
}

func (h *UpstreamGovernanceHandler) ModelRuns(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	page, size, ok := governancePage(c)
	if !ok {
		return
	}
	batch := c.Query("batch_id")
	if batch != "" {
		if _, ok := governanceModelUUID(c, batch); !ok {
			return
		}
	}
	value, err := h.svc.ListModelRuns(c.Request.Context(), id, page, size, batch)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ModelRun(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	run, ok := governanceModelUUID(c, c.Param("run_id"))
	if !ok {
		return
	}
	value, err := h.svc.GetModelRun(c.Request.Context(), id, run)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

// DeleteModelRun hides one model-monitoring result from the user-facing IQ
// projection while retaining the run in the administrator workbench.
func (h *UpstreamGovernanceHandler) DeleteModelRun(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	run, ok := governanceModelUUID(c, c.Param("run_id"))
	if !ok {
		return
	}
	if !governanceError(c, h.svc.HideModelRun(c.Request.Context(), id, run)) {
		response.Success(c, gin.H{"deleted": true})
	}
}

func (h *UpstreamGovernanceHandler) ReviewModelRun(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	run, ok := governanceModelUUID(c, c.Param("run_id"))
	if !ok {
		return
	}
	var input struct {
		Review string `json:"review"`
		Note   string `json:"note"`
	}
	if !governanceBody(c, &input) {
		return
	}
	if (input.Review != "pass" && input.Review != "fail" && input.Review != "pending") || len(input.Note) > 4000 || strings.ContainsRune(input.Note, 0) {
		governanceError(c, gov.ErrInvalid)
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator identity is required")
		return
	}
	value, err := h.svc.ReviewModelRun(c.Request.Context(), id, run, input.Review, input.Note, subject.UserID)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}
