package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDownstreamUsageProjectionMiddlewareReportsBufferedJSONSizeAndWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name  string
		write func(c *gin.Context)
	}{
		{
			name: "JSON",
			write: func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{
					"usage": gin.H{
						"prompt_tokens":     100,
						"completion_tokens": 10,
						"total_tokens":      110,
					},
				})
			},
		},
		{
			name: "Data",
			write: func(c *gin.Context) {
				c.Data(http.StatusOK, "application/json", []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110}}`))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(service.WithDownstreamUsageAdjustment(
					c.Request.Context(),
					service.TokenUsageAdjustment{UnifiedMultiplier: 2},
				))
				c.Next()
			})
			r.Use(DownstreamUsageProjection())

			var sizeAfterWrite int
			var writtenAfterWrite bool
			r.POST("/v1/chat/completions", func(c *gin.Context) {
				tc.write(c)
				sizeAfterWrite = c.Writer.Size()
				writtenAfterWrite = c.Writer.Written()
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if sizeAfterWrite < 0 {
				t.Fatalf("Size() after buffered JSON write = %d, want committed bytes (not -1)", sizeAfterWrite)
			}
			if !writtenAfterWrite {
				t.Fatal("Written() after buffered JSON write = false, want true")
			}
		})
	}
}

func TestDownstreamUsageProjectionMiddlewareReportsIncompleteSSEPendingSize(t *testing.T) {
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

	chunk := "data: {"
	var sizeAfterWrite int
	var writtenAfterWrite bool
	r.POST("/v1/responses", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString(chunk)
		sizeAfterWrite = c.Writer.Size()
		writtenAfterWrite = c.Writer.Written()
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if sizeAfterWrite != len(chunk) {
		t.Fatalf("Size() after incomplete SSE chunk = %d, want %d (pending counts)", sizeAfterWrite, len(chunk))
	}
	if !writtenAfterWrite {
		t.Fatal("Written() after incomplete SSE chunk = false, want true")
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

func TestDownstreamUsageProjectionMiddlewareRewritesAnthropicJSONWithUnifiedMultiplier(t *testing.T) {
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
	r.POST("/v1/messages", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"usage": gin.H{
				"input_tokens":                80,
				"output_tokens":               10,
				"cache_read_input_tokens":     20,
				"cache_creation_input_tokens": 5,
			},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is invalid JSON: %v body=%s", err, rec.Body.String())
	}
	if body.Usage.InputTokens != 160 || body.Usage.OutputTokens != 20 ||
		body.Usage.CacheReadInputTokens != 40 || body.Usage.CacheCreationInputTokens != 10 {
		t.Fatalf("projected usage = %+v, want 160/20/40/10", body.Usage)
	}
}

func TestDownstreamUsageProjectionMiddlewareRewritesSSEUsageFramesWithUnifiedMultiplier(t *testing.T) {
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
	r.POST("/v1/responses", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: {\"usage\":{\"output_tokens\":10}}\n\n")
		_, _ = c.Writer.WriteString("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":10,\"total_tokens\":110}}}\n\n")
		_, _ = c.Writer.WriteString("data: {\"usage\":{\"output_tokens\":10}}\n\n")
		c.Writer.Flush()
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	payloads := sseDataPayloads(t, rec.Body.String())
	if len(payloads) != 3 {
		t.Fatalf("sse payloads = %d, want 3 body=%q", len(payloads), rec.Body.String())
	}
	if got := jsonInt(usageField(payloads[0], "output_tokens")); got != 10 {
		t.Fatalf("ineligible output frame output_tokens = %v, want 10", payloads[0])
	}
	completed := usageObject(payloads[1])
	if jsonInt(completed["input_tokens"]) != 200 || jsonInt(completed["output_tokens"]) != 20 || jsonInt(completed["total_tokens"]) != 220 {
		t.Fatalf("eligible input frame usage = %v, want 200/20/220", completed)
	}
	if got := jsonInt(usageField(payloads[2], "output_tokens")); got != 20 {
		t.Fatalf("eligible output frame output_tokens = %v, want 20", payloads[2])
	}
}

func TestDownstreamUsageProjectionMiddlewareSkipsTokenCountEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"usage":{"input_tokens":80,"output_tokens":10}}`
	for _, path := range []string{"/v1/messages/count_tokens", "/v1/responses/input_tokens"} {
		t.Run(path, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(service.WithDownstreamUsageAdjustment(
					c.Request.Context(),
					service.TokenUsageAdjustment{UnifiedMultiplier: 2},
				))
				c.Next()
			})
			r.Use(DownstreamUsageProjection())
			r.POST(path, func(c *gin.Context) {
				c.Data(http.StatusOK, "application/json", []byte(body))
			})

			req := httptest.NewRequest(http.MethodPost, path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if got := rec.Body.String(); got != body {
				t.Fatalf("response = %s, want unchanged usage", got)
			}
		})
	}
}

func TestDownstreamUsageProjectionMiddlewareLeavesGeminiNativeUsageMetadataUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"usageMetadata":{"promptTokenCount":80,"candidatesTokenCount":10,"totalTokenCount":90}}`
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithDownstreamUsageAdjustment(
			c.Request.Context(),
			service.TokenUsageAdjustment{UnifiedMultiplier: 2},
		))
		c.Next()
	})
	r.Use(DownstreamUsageProjection())
	r.POST("/v1beta/models/*modelAction", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", []byte(body))
	})

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != body {
		t.Fatalf("gemini native response = %s, want usageMetadata unchanged", got)
	}
}

func sseDataPayloads(t *testing.T, body string) []map[string]any {
	t.Helper()
	var payloads []map[string]any
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" || !strings.HasPrefix(frame, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(frame, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(payload), &obj); err != nil {
			t.Fatalf("sse payload %q: %v", payload, err)
		}
		payloads = append(payloads, obj)
	}
	return payloads
}

func usageObject(payload map[string]any) map[string]any {
	if usage, ok := payload["usage"].(map[string]any); ok {
		return usage
	}
	if response, ok := payload["response"].(map[string]any); ok {
		if usage, ok := response["usage"].(map[string]any); ok {
			return usage
		}
	}
	return map[string]any{}
}

func usageField(payload map[string]any, key string) any {
	return usageObject(payload)[key]
}

func jsonInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case int:
		return typed
	default:
		return 0
	}
}
