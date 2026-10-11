package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestDownstreamUsageProjectionMiddlewareRewritesJSONResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithDownstreamUsageAdjustment(
			c.Request.Context(),
			service.TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2},
		))
		c.Next()
	})
	r.Use(DownstreamUsageProjection())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"usage": gin.H{
				"prompt_tokens":     100,
				"completion_tokens": 10,
				"total_tokens":      110,
			},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"usage":{"completion_tokens":20,"prompt_tokens":200,"total_tokens":220}}` {
		t.Fatalf("response = %s, want projected usage", got)
	}
}

func TestDownstreamUsageProjectionMiddlewareRewritesJSONResponseWithUnifiedMultiplier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithDownstreamUsageAdjustment(
			c.Request.Context(),
			service.TokenUsageAdjustment{UnifiedMultiplier: 2},
		))
		c.Next()
	})
	r.Use(DownstreamUsageProjection())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"usage": gin.H{
				"prompt_tokens":     100,
				"completion_tokens": 10,
				"total_tokens":      110,
			},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"usage":{"completion_tokens":20,"prompt_tokens":200,"total_tokens":220}}` {
		t.Fatalf("response = %s, want projected usage", got)
	}
}

func TestDownstreamUsageProjectionMiddlewareRewritesSSEUsageFrames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithDownstreamUsageAdjustment(
			c.Request.Context(),
			service.TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2},
		))
		c.Next()
	})
	r.Use(DownstreamUsageProjection())
	r.POST("/v1/responses", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":10,\"total_tokens\":110}}}\n\n")
		c.Writer.Flush()
		_, _ = c.Writer.WriteString("data: [DONE]\n\n")
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "data: {\"response\":{\"usage\":{\"input_tokens\":200,\"output_tokens\":20,\"total_tokens\":220}},\"type\":\"response.completed\"}\n\ndata: [DONE]\n\n" {
		t.Fatalf("response = %q, want projected SSE usage", got)
	}
}

func TestDownstreamUsageProjectionMiddlewareLeavesUsageEndpointUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithDownstreamUsageAdjustment(
			c.Request.Context(),
			service.TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2},
		))
		c.Next()
	})
	r.Use(DownstreamUsageProjection())
	r.POST("/v1/usage", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"usage": gin.H{"input_tokens": 100, "output_tokens": 10}})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/usage", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != `{"usage":{"input_tokens":100,"output_tokens":10}}` {
		t.Fatalf("usage endpoint response = %s, want unchanged", got)
	}
}
