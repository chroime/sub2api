package upstreamgovernance

// Transport identifies the local API-key account platform. Several platforms
// share OpenAI's wire format, but retain their own scheduling and group identity.
// Composite is a group router, not an account platform.
func validTransport(platform string) bool {
	switch platform {
	case "openai", "anthropic", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go":
		return true
	default:
		return false
	}
}

func validSiteTransport(sitePlatform, platform string) bool {
	if !validTransport(platform) {
		return false
	}
	// Native API-key Antigravity accounts call Sub2API's /antigravity namespace;
	// New API exposes the public wire protocols without this routing namespace.
	return platform != "antigravity" || sitePlatform == "sub2api"
}

func compatibleTransport(remote, selected string) bool {
	if !validTransport(selected) {
		return false
	}
	switch remote {
	case "", "unknown", "composite":
		return true
	case "grok":
		// Keep previously imported Grok/OpenAI markers and managed keys usable.
		// New Grok imports can use their native platform without rewriting them.
		return selected == "grok" || selected == "openai"
	default:
		return remote == selected
	}
}
