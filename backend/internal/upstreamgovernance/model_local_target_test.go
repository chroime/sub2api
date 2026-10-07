package upstreamgovernance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func localModelConfigFixture() ModelTestConfig {
	return ModelTestConfig{
		TargetType:            "local_group",
		LocalGroupID:          7,
		LocalAPIKeyID:         11,
		TargetOwnerUserID:     42,
		Platform:              "openai",
		Model:                 "gpt-6",
		APIMode:               "chat_completions",
		Efforts:               []string{"medium"},
		Templates:             []string{"candy"},
		Samples:               1,
		Concurrency:           1,
		MaxOutputTokens:       128,
		TimeoutSeconds:        30,
		InputTokens:           128,
		Tokenizer:             "auto",
		TokenTolerancePercent: 10,
	}
}

func TestValidateModelConfigAllowsLocalGroupWithoutManagedKey(t *testing.T) {
	c := localModelConfigFixture()
	require.NoError(t, validateModelConfig(&c))
}

func TestValidateModelConfigRejectsIncompleteLocalGroupTarget(t *testing.T) {
	c := localModelConfigFixture()
	for name, mutate := range map[string]func(*ModelTestConfig){
		"missing group": func(v *ModelTestConfig) { v.LocalGroupID = 0 },
		"missing key":   func(v *ModelTestConfig) { v.LocalAPIKeyID = 0 },
		"missing owner": func(v *ModelTestConfig) { v.TargetOwnerUserID = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			value := c
			mutate(&value)
			require.ErrorIs(t, validateModelConfig(&value), ErrInvalid)
		})
	}
}

func TestValidateModelConfigRejectsUnknownTargetType(t *testing.T) {
	c := localModelConfigFixture()
	c.TargetType = "remote_url"
	require.ErrorIs(t, validateModelConfig(&c), ErrInvalid)
}

func TestModelExecutionErrorMapsLocalTargetFailures(t *testing.T) {
	require.Equal(t, "target_missing", modelExecutionError(ErrNotFound))
	require.Equal(t, "target_changed", modelExecutionError(ErrConflict))
}

func TestLocalGatewayModelRunnerUsesLoopbackGatewayAndAPIKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "gpt-6",
			"choices": []any{map[string]any{"message": map[string]any{"content": "21"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 2},
		})
	}))
	defer server.Close()
	runner, err := NewLocalGatewayModelRunner(server.URL)
	require.NoError(t, err)
	result, err := runner.RunLocalModel(t.Context(), LocalModelTarget{GroupID: 7, APIKeyID: 11, Key: "local-secret"}, ModelRunRequest{
		Config:   ModelTestConfig{Platform: "openai", Model: "gpt-6", APIMode: "chat_completions", Efforts: []string{"medium"}, Templates: []string{"candy"}, Samples: 1, Concurrency: 1, MaxOutputTokens: 64, TimeoutSeconds: 5, InputTokens: 32, Tokenizer: "auto", TokenTolerancePercent: 10},
		Template: "candy", Effort: "medium", Sample: 1,
	})
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "Bearer local-secret", gotAuth)
}

func TestLocalGatewayModelRunnerRejectsNonLoopbackURL(t *testing.T) {
	_, err := NewLocalGatewayModelRunner("https://example.com:443")
	require.ErrorIs(t, err, ErrInvalid)
}
