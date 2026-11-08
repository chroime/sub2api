package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type groupInputRoutingReasoningRouteKey struct{}
type groupInputRoutingReasoningAppliedKey struct{}

type groupInputRoutingReasoningRoute struct {
	platform string
	model    string
}

// BindGroupInputRoutingReasoningRoute preserves only the source route's policy
// semantics. It must not set the platform/model used for scheduling or billing.
func BindGroupInputRoutingReasoningRoute(c *gin.Context, decision service.CompositeRouteDecision) {
	if c == nil || c.Request == nil || !decision.Matched {
		return
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), groupInputRoutingReasoningRouteKey{}, groupInputRoutingReasoningRoute{
		platform: decision.TargetPlatform,
		model:    decision.UpstreamModel,
	}))
}

// applyGroupInputRoutingReasoningEffortPolicy enforces known source policies
// before billing/selection. Only an unresolved composite source is deferred to
// the selected account. Deferred policies never mutate the retry template or
// mark the whole request applied, so each attempt applies one mapping once.
func applyGroupInputRoutingReasoningEffortPolicy(c *gin.Context, apiKey *service.APIKey, body []byte, selectedPlatform string) ([]byte, bool, error) {
	if c == nil || c.Request == nil || apiKey == nil || apiKey.Group == nil {
		return body, false, nil
	}
	ctx := c.Request.Context()
	if _, matched := service.GroupInputRoutingDecisionFromContext(ctx); !matched {
		return body, false, nil
	}
	if applied, _ := ctx.Value(groupInputRoutingReasoningAppliedKey{}).(bool); applied {
		return body, false, nil
	}
	group := apiKey.Group
	platform := group.Platform
	policyModel, _ := service.RequestedPublicModelFromContext(ctx)
	if policyModel == "" {
		policyModel = gjson.GetBytes(body, "model").String()
	}
	if platform == service.PlatformComposite {
		if route, ok := ctx.Value(groupInputRoutingReasoningRouteKey{}).(groupInputRoutingReasoningRoute); ok {
			platform = route.platform
			if route.model != "" {
				policyModel = route.model
			}
		} else if resolved, ok := service.ResolvedTargetPlatformFromContext(ctx); ok {
			platform = resolved
			if model, ok := service.ResolvedUpstreamModelFromContext(ctx); ok {
				policyModel = model
			}
		} else if selectedPlatform != "" {
			platform = selectedPlatform
		} else {
			return body, false, nil
		}
	}
	if selectedPlatform == "" {
		c.Request = c.Request.WithContext(context.WithValue(ctx, groupInputRoutingReasoningAppliedKey{}, true))
	}
	if platform != service.PlatformAnthropic && platform != service.PlatformOpenAI {
		return body, false, nil
	}
	if service.RequestedReasoningEffortFromContext(c.Request.Context()) == nil {
		bindRequestedReasoningEffort(c, body, gjson.GetBytes(body, "model").String())
	}
	maxEffort, mappings := group.MaxReasoningEffort, group.ReasoningEffortMappings
	if platform == service.PlatformAnthropic {
		maxEffort, mappings = anthropicCompatibleReasoningEffortPolicy(maxEffort, mappings)
	}
	originalModel := gjson.GetBytes(body, "model")
	policyBody := body
	if strings.TrimSpace(policyModel) != "" && policyModel != originalModel.String() {
		var err error
		policyBody, err = sjson.SetBytes(body, "model", policyModel)
		if err != nil {
			return body, false, err
		}
	}
	updated, changed, err := service.ApplyReasoningEffortPolicy(policyBody, maxEffort, mappings, group.MaxReasoningEffortOverLimit)
	if err != nil || !changed {
		return body, false, err
	}
	if policyModel != originalModel.String() {
		if originalModel.Exists() {
			updated, err = sjson.SetRawBytes(updated, "model", []byte(originalModel.Raw))
		} else {
			updated, err = sjson.DeleteBytes(updated, "model")
		}
		if err != nil {
			return body, false, err
		}
	}
	return updated, true, nil
}
