package service

import (
	"encoding/json"
	"testing"
)

func TestProjectDownstreamUsageJSONScalesOpenAIChatUsage(t *testing.T) {
	input := []byte(`{"id":"chatcmpl-test","usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":5}}}`)
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2}

	projected, changed := ProjectDownstreamUsageJSON(input, adjustment)
	if !changed {
		t.Fatal("expected usage projection to change the response")
	}

	var body struct {
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			TotalTokens         int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens     int `json:"cached_tokens"`
				CacheWriteTokens int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("projected response is invalid JSON: %v", err)
	}
	if body.Usage.PromptTokens != 200 || body.Usage.CompletionTokens != 20 || body.Usage.TotalTokens != 220 {
		t.Fatalf("projected usage = %+v, want prompt=200 completion=20 total=220", body.Usage)
	}
	if body.Usage.PromptTokensDetails.CachedTokens != 40 {
		t.Fatalf("cached tokens = %d, want 40", body.Usage.PromptTokensDetails.CachedTokens)
	}
	if body.Usage.PromptTokensDetails.CacheWriteTokens != 10 {
		t.Fatalf("cache write tokens = %d, want 10", body.Usage.PromptTokensDetails.CacheWriteTokens)
	}
}

func TestProjectDownstreamUsageJSONScalesAnthropicCacheBuckets(t *testing.T) {
	input := []byte(`{"usage":{"input_tokens":80,"output_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}`)
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2}

	projected, changed := ProjectDownstreamUsageJSON(input, adjustment)
	if !changed {
		t.Fatal("expected usage projection to change the response")
	}

	var body struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("projected response is invalid JSON: %v", err)
	}
	if body.Usage.InputTokens != 160 || body.Usage.OutputTokens != 20 ||
		body.Usage.CacheReadInputTokens != 40 || body.Usage.CacheCreationInputTokens != 10 {
		t.Fatalf("projected usage = %+v, want input=160 output=20 read=40 creation=10", body.Usage)
	}
}

func TestProjectDownstreamUsageJSONHonorsMinimumInputThreshold(t *testing.T) {
	input := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":20}}}`)
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2, MinInputTokens: 81}

	projected, changed := ProjectDownstreamUsageJSON(input, adjustment)
	if changed {
		t.Fatal("short non-cache input should not be projected")
	}
	if string(projected) != string(input) {
		t.Fatalf("response changed below threshold: %s", projected)
	}
}

func TestProjectDownstreamUsageJSONProjectsNestedResponsesUsage(t *testing.T) {
	input := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":10,"total_tokens":110,"input_tokens_details":{"cached_tokens":20}}}}`)
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2}

	projected, changed := ProjectDownstreamUsageJSON(input, adjustment)
	if !changed {
		t.Fatal("expected nested response usage projection")
	}

	var body struct {
		Response struct {
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("projected response is invalid JSON: %v", err)
	}
	if body.Response.Usage.InputTokens != 200 || body.Response.Usage.OutputTokens != 20 || body.Response.Usage.TotalTokens != 220 {
		t.Fatalf("nested usage = %+v, want input=200 output=20 total=220", body.Response.Usage)
	}
}

func TestProjectDownstreamUsageJSONWithEligibilityProjectsAnthropicOutputFrame(t *testing.T) {
	eligible := false
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2, MinInputTokens: 81}
	shortOutput, changed := ProjectDownstreamUsageJSONWithEligibility(
		[]byte(`{"usage":{"output_tokens":10}}`), adjustment, &eligible,
	)
	if changed || string(shortOutput) != `{"usage":{"output_tokens":10}}` {
		t.Fatalf("ineligible output frame changed: changed=%v body=%s", changed, shortOutput)
	}

	inputFrame, changed := ProjectDownstreamUsageJSONWithEligibility(
		[]byte(`{"usage":{"input_tokens":100,"output_tokens":0,"cache_read_input_tokens":20}}`), adjustment, &eligible,
	)
	if !changed || !eligible {
		t.Fatalf("eligible input frame was not projected: changed=%v eligible=%v body=%s", changed, eligible, inputFrame)
	}

	outputFrame, changed := ProjectDownstreamUsageJSONWithEligibility(
		[]byte(`{"usage":{"output_tokens":10}}`), adjustment, &eligible,
	)
	if !changed || string(outputFrame) != `{"usage":{"output_tokens":20}}` {
		t.Fatalf("eligible output frame = %s, changed=%v, want output=20", outputFrame, changed)
	}
}

func TestProjectDownstreamUsageJSONClampsCacheDetailsToProjectedInput(t *testing.T) {
	input := []byte(`{"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":1,"cache_write_tokens":1}}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{InputMultiplier: 1.5, OutputMultiplier: 1})
	if !changed {
		t.Fatal("expected usage projection")
	}
	var body struct {
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			Details      struct {
				CachedTokens     int `json:"cached_tokens"`
				CacheWriteTokens int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("projected response is invalid JSON: %v", err)
	}
	if body.Usage.Details.CachedTokens+body.Usage.Details.CacheWriteTokens > body.Usage.PromptTokens {
		t.Fatalf("cache details exceed projected input: %+v", body.Usage)
	}
}

func TestProjectDownstreamUsageJSONSynchronizesCompatibilityUsageShapes(t *testing.T) {
	input := []byte(`{"usage":{"input_tokens":100,"output_tokens":10,"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"cached_tokens":80}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2})
	if !changed {
		t.Fatal("expected compatibility usage projection")
	}
	var body struct {
		Usage struct {
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			CachedTokens     int `json:"cached_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("projected response is invalid JSON: %v", err)
	}
	if body.Usage.InputTokens != 200 || body.Usage.PromptTokens != 200 ||
		body.Usage.OutputTokens != 20 || body.Usage.CompletionTokens != 20 || body.Usage.CachedTokens != 160 {
		t.Fatalf("compatibility usage = %+v, want input/prompt=200 output/completion=20 cached=160", body.Usage)
	}
}

func TestProjectDownstreamUsageJSONScalesMixedUsageOutputAlias(t *testing.T) {
	input := []byte(`{"usage":{"input_tokens":100,"output_tokens":10,"prompt_tokens":100,"total_tokens":110,"cached_tokens":20}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2})
	if !changed {
		t.Fatal("expected mixed usage projection")
	}
	var body struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("projected response is invalid JSON: %v", err)
	}
	if body.Usage.InputTokens != 200 || body.Usage.PromptTokens != 200 ||
		body.Usage.OutputTokens != 20 || body.Usage.TotalTokens != 220 {
		t.Fatalf("mixed usage = %+v, want input/prompt=200 output=20 total=220", body.Usage)
	}
}

func TestProjectDownstreamUsageJSONUsesOneThresholdForMixedUsageAliases(t *testing.T) {
	input := []byte(`{"usage":{"input_tokens":100,"output_tokens":10,"prompt_tokens":100,"cache_read_input_tokens":80}}`)
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2, MinInputTokens: 50}
	projected, changed := ProjectDownstreamUsageJSON(input, adjustment)
	if changed {
		t.Fatal("mixed usage below the canonical non-cache threshold should remain unchanged")
	}
	if string(projected) != string(input) {
		t.Fatalf("response changed below threshold: %s", projected)
	}
}

func TestProjectDownstreamUsageJSONScalesNestedAnthropicCacheCreation(t *testing.T) {
	input := []byte(`{"usage":{"input_tokens":100,"output_tokens":10,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":70}}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2})
	if !changed {
		t.Fatal("expected nested cache creation projection")
	}
	var body struct {
		Usage struct {
			InputTokens   int `json:"input_tokens"`
			OutputTokens  int `json:"output_tokens"`
			CacheCreation struct {
				FiveMinute int `json:"ephemeral_5m_input_tokens"`
				OneHour    int `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("projected response is invalid JSON: %v", err)
	}
	if body.Usage.InputTokens != 200 || body.Usage.OutputTokens != 20 ||
		body.Usage.CacheCreation.FiveMinute != 60 || body.Usage.CacheCreation.OneHour != 140 {
		t.Fatalf("nested cache usage = %+v, want input=200 output=20 cache=60/140", body.Usage)
	}
}

func TestProjectDownstreamUsageJSONDoesNotDoubleScaleMixedNestedCacheCreation(t *testing.T) {
	input := []byte(`{"usage":{"input_tokens":100,"output_tokens":10,"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cache_write_tokens":100},"input_tokens_details":{"cache_write_tokens":100},"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":70}}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2})
	if !changed {
		t.Fatal("expected mixed usage projection")
	}
	var body struct {
		Usage struct {
			CacheCreation struct {
				FiveMinute int `json:"ephemeral_5m_input_tokens"`
				OneHour    int `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("projected response is invalid JSON: %v", err)
	}
	if body.Usage.CacheCreation.FiveMinute != 60 || body.Usage.CacheCreation.OneHour != 140 {
		t.Fatalf("nested cache creation = %+v, want 60/140", body.Usage.CacheCreation)
	}
}

func TestProjectDownstreamUsageJSONUnifiedScalesAnthropicCacheBuckets(t *testing.T) {
	input := []byte(`{"usage":{"input_tokens":80,"output_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{UnifiedMultiplier: 2})
	if !changed {
		t.Fatal("expected unified usage projection")
	}
	var body struct {
		Usage struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Usage.InputTokens != 160 || body.Usage.OutputTokens != 20 || body.Usage.CacheReadTokens != 40 || body.Usage.CacheCreationTokens != 10 {
		t.Fatalf("projected = %+v, want 160/20/40/10", body.Usage)
	}
}

func TestProjectDownstreamUsageJSONUnifiedScalesOpenAICachedTokens(t *testing.T) {
	input := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":5}}}`)
	projected, changed := ProjectDownstreamUsageJSON(input, TokenUsageAdjustment{UnifiedMultiplier: 2})
	if !changed {
		t.Fatal("expected unified usage projection")
	}
	var body struct {
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			TotalTokens         int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens     int `json:"cached_tokens"`
				CacheWriteTokens int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Usage.PromptTokens != 200 || body.Usage.CompletionTokens != 20 || body.Usage.TotalTokens != 220 {
		t.Fatalf("openai totals = %+v", body.Usage)
	}
	if body.Usage.PromptTokensDetails.CachedTokens != 40 || body.Usage.PromptTokensDetails.CacheWriteTokens != 10 {
		t.Fatalf("openai cache = %+v", body.Usage.PromptTokensDetails)
	}
}

func TestProjectDownstreamUsageJSONWithEligibilityProjectsUnifiedOutputFrame(t *testing.T) {
	eligible := false
	adjustment := TokenUsageAdjustment{UnifiedMultiplier: 2, MinInputTokens: 81}
	shortOutput, changed := ProjectDownstreamUsageJSONWithEligibility(
		[]byte(`{"usage":{"output_tokens":10}}`), adjustment, &eligible,
	)
	if changed || string(shortOutput) != `{"usage":{"output_tokens":10}}` {
		t.Fatalf("ineligible output frame changed: changed=%v body=%s", changed, shortOutput)
	}

	inputFrame, changed := ProjectDownstreamUsageJSONWithEligibility(
		[]byte(`{"usage":{"input_tokens":100,"output_tokens":0,"cache_read_input_tokens":20}}`), adjustment, &eligible,
	)
	if !changed || !eligible {
		t.Fatalf("eligible input frame was not projected: changed=%v eligible=%v body=%s", changed, eligible, inputFrame)
	}

	outputFrame, changed := ProjectDownstreamUsageJSONWithEligibility(
		[]byte(`{"usage":{"output_tokens":10}}`), adjustment, &eligible,
	)
	if !changed || string(outputFrame) != `{"usage":{"output_tokens":20}}` {
		t.Fatalf("eligible unified output frame = %s, changed=%v, want output=20", outputFrame, changed)
	}
}
