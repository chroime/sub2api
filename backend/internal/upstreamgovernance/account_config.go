package upstreamgovernance

import (
	"math"
	"strings"
)

// NormalizeAccountConfig copies the requested settings before they are frozen
// into a preview. Nil means the governance import defaults, including the
// upstream's advertised models; an explicit empty mapping allows every model.
func NormalizeAccountConfig(input *AccountConfig, platform string, models []string) (*AccountConfig, error) {
	config := AccountConfig{
		Concurrency: 5000, UpstreamBillingRateSyncEnabled: true,
		QuotaDailyLimit: 10000, QuotaWeeklyLimit: 700000, QuotaLimit: 10000000,
		OpenAILongContextBillingEnabled: platform == "openai",
	}
	if input != nil {
		config = *input
	}
	priority := 1
	if config.Priority != nil {
		priority = *config.Priority
	}
	if priority < 0 || int64(priority) > math.MaxInt32 {
		return nil, ErrInvalid
	}
	config.Priority = &priority
	config.ModelMapping = make(map[string]string)
	if input == nil {
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model != "" && !strings.Contains(model, "*") {
				config.ModelMapping[model] = model
			}
		}
	} else {
		for from, to := range input.ModelMapping {
			from, to = strings.TrimSpace(from), strings.TrimSpace(to)
			if from == "" || to == "" || strings.Contains(to, "*") || strings.Count(from, "*") > 1 || (strings.Contains(from, "*") && !strings.HasSuffix(from, "*")) {
				return nil, ErrInvalid
			}
			if previous, exists := config.ModelMapping[from]; exists && previous != to {
				return nil, ErrInvalid
			}
			config.ModelMapping[from] = to
		}
	}
	if config.Concurrency < 1 || int64(config.Concurrency) > math.MaxInt32 || !validRate(config.QuotaDailyLimit) || !validRate(config.QuotaWeeklyLimit) || !validRate(config.QuotaLimit) {
		return nil, ErrInvalid
	}
	if platform != "openai" {
		config.OpenAILongContextBillingEnabled = false
	}
	return &config, nil
}
