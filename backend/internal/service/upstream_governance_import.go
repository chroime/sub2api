package service

import gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"

// Governance freezes an explicit model allowlist in each import preview. Native
// provider defaults and aliases must not silently expand that list; an empty
// list means the administrator selected unrestricted upstream models.
func (account *Account) usesGovernanceModelPolicy() bool {
	return account != nil && account.Type == AccountTypeAPIKey && account.GetExtraString(governanceMarkerKey) != ""
}

func governanceAccountConfig(account *Account) *gov.AccountConfig {
	priority := account.Priority
	mapping := stringMappingFromRaw(account.Credentials["model_mapping"])
	if mapping == nil {
		mapping = map[string]string{}
	}
	return &gov.AccountConfig{
		Concurrency: account.Concurrency, Priority: &priority, ModelMapping: mapping,
		UpstreamBillingRateSyncEnabled: upstreamBillingRateSyncEnabled(account),
		QuotaDailyLimit:                account.GetQuotaDailyLimit(), QuotaWeeklyLimit: account.GetQuotaWeeklyLimit(), QuotaLimit: account.GetQuotaLimit(),
		OpenAILongContextBillingEnabled: account.IsOpenAILongContextBillingEnabled(),
	}
}

func governanceImportCredentials(previous map[string]any, change gov.AccountChange) map[string]any {
	credentials := make(map[string]any, len(previous)+3)
	for key, value := range previous {
		credentials[key] = value
	}
	credentials["api_key"], credentials["base_url"] = change.APIKey, change.BaseURL
	// Relay imports use the public Chat Completions endpoint. In particular,
	// OpenCode's adaptive default would otherwise choose official protocol URLs
	// that do not belong to this upstream. Preserve an administrator's explicit
	// protocol when updating an existing governed account.
	if (IsCNProvider(change.Platform) || change.Platform == PlatformOpenCodeGo) && credentials["api_protocol"] == nil {
		credentials["api_protocol"] = APIProtocolChatCompletions
	}
	delete(credentials, "model_mapping")
	if len(change.AccountConfig.ModelMapping) > 0 {
		mapping := make(map[string]any, len(change.AccountConfig.ModelMapping))
		for from, to := range change.AccountConfig.ModelMapping {
			mapping[from] = to
		}
		credentials["model_mapping"] = mapping
	}
	return credentials
}

func governanceImportExtra(previous map[string]any, change gov.AccountChange) map[string]any {
	extra := make(map[string]any, len(previous)+5)
	for key, value := range previous {
		extra[key] = value
	}
	extra[governanceMarkerKey] = change.Marker
	// Billing switches use typed input fields; copied old switches would conflict
	// when the reviewed import deliberately changes their state.
	delete(extra, UpstreamBillingProbeEnabledExtraKey)
	delete(extra, UpstreamBillingRateSyncEnabledExtraKey)
	for key, value := range map[string]float64{
		"quota_daily_limit":  change.AccountConfig.QuotaDailyLimit,
		"quota_weekly_limit": change.AccountConfig.QuotaWeeklyLimit,
		"quota_limit":        change.AccountConfig.QuotaLimit,
	} {
		if value > 0 {
			extra[key] = value
		} else {
			delete(extra, key)
		}
	}
	if change.Platform == PlatformOpenAI {
		extra[openAILongContextBillingEnabledKey] = change.AccountConfig.OpenAILongContextBillingEnabled
	}
	return extra
}
