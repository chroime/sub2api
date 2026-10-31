package handler

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const grokForbiddenReplayClientMessage = "This request was recently rejected by the upstream (403). Stop retrying it and adjust the request before trying again."

// beginGrokForbiddenReplayGuard protects pooled Grok accounts from downstream
// clients that blindly replay requests xAI refused with 403 (403s are billed).
// It returns blocked=true after writing a local 429 when the request must not
// reach any account. Otherwise the returned finish func must be deferred so the
// request outcome is committed once the failover loop is done.
func beginGrokForbiddenReplayGuard(
	c *gin.Context,
	reqLog *zap.Logger,
	apiKeyID int64,
	requestPlatform string,
	body []byte,
	writeError func(c *gin.Context, status int, errType, message string),
) (finish func(), blocked bool) {
	noop := func() {}
	if c == nil || c.Request == nil || requestPlatform != service.PlatformGrok {
		return noop, false
	}
	guard := service.NewGrokForbiddenReplayGuard(apiKeyID, c.Request.URL.Path, body)
	if wait, scope, ok := guard.Blocked(time.Now()); ok {
		retryAfter := int(math.Ceil(wait.Seconds()))
		if retryAfter < 1 {
			retryAfter = 1
		}
		if reqLog != nil {
			reqLog.Warn("grok.forbidden_replay_blocked",
				zap.String("scope", scope),
				zap.Int("retry_after_seconds", retryAfter),
			)
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		writeError(c, http.StatusTooManyRequests, "rate_limit_error", grokForbiddenReplayClientMessage)
		return noop, true
	}
	c.Request = c.Request.WithContext(service.WithGrokForbiddenReplayGuard(c.Request.Context(), guard))
	return func() {
		clientGone := c.Request.Context().Err() != nil
		if guard.Finish(clientGone, time.Now()) && reqLog != nil {
			reqLog.Info("grok.forbidden_replay_recorded")
		}
	}, false
}
