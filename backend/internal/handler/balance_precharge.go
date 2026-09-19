package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Keep the connection alive only while admission is waiting. The service invokes
// this callback on the handler goroutine, before account selection or forwarding,
// so it never races the upstream writer or starts an account-controlled ACK.
func balancePrechargeWaitContext(ctx context.Context, c *gin.Context, isSSE bool, streamStarted *bool) context.Context {
	if !isSSE || c == nil || c.Request == nil || c.Writer == nil || streamStarted == nil {
		return ctx
	}
	return service.WithBalancePrechargeWaitHeartbeat(ctx, balancePrechargeWaitHeartbeat(c, streamStarted))
}

func balancePrechargeWaitHeartbeat(c *gin.Context, streamStarted *bool) func() error {
	return func() error {
		if err := c.Request.Context().Err(); err != nil {
			return err
		}
		if !*streamStarted {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Header("X-Accel-Buffering", "no")
		}
		written, err := fmt.Fprint(c.Writer, ": keepalive\n\n")
		recordGatewayStreamHeartbeat(c, written)
		if written > 0 {
			*streamStarted = true
		}
		if err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}
}

func gatewaySSEAlreadyStarted(c *gin.Context) bool {
	return c != nil && c.Writer != nil && c.Writer.Written() && strings.Contains(c.Writer.Header().Get("Content-Type"), "text/event-stream")
}

// BalancePrechargeLifecycle attaches ownership only to generation requests whose
// usage is settled in the same request/worker lifecycle. Async jobs have their
// own durable workflow and must never be refunded on HTTP submission completion.
func (h *GatewayHandler) BalancePrechargeLifecycle() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h == nil || h.gatewayService == nil || !balancePrechargeRequestPath(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		ctx := h.gatewayService.WithBalancePrecharge(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)
		defer service.FinishBalancePrecharge(ctx)
		c.Next()
	}
}

func balancePrechargeRequestPath(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	for _, suffix := range []string{"/messages", "/chat/completions", "/responses", "/responses/compact", "/embeddings", "/images/generations", "/images/edits", "/alpha/search", "/web_search", "/x_search", "/tts", "/stt"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent")
}
