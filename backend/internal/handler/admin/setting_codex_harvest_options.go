package admin

import "github.com/Wei-Shaw/sub2api/internal/config"

var codexTicketHarvestSettingsJSONKeys = []string{
	"openai_codex_ticket_verify_enabled", "openai_codex_ticket_harvest_proxy_ids", "openai_codex_ticket_harvest_concurrency",
	"openai_codex_ticket_332_verify_enabled", "openai_codex_ticket_332_harvest_proxy_ids", "openai_codex_ticket_332_harvest_concurrency",
}

func validateCodexTicketHarvestOptionsRequest(req UpdateSettingsRequest) error {
	for _, option := range []struct {
		ids         *[]int64
		concurrency *int
	}{
		{req.OpenAICodexTicketHarvestProxyIDs, req.OpenAICodexTicketHarvestConcurrency},
		{req.OpenAICodexTicket332HarvestProxyIDs, req.OpenAICodexTicket332HarvestConcurrency},
	} {
		concurrency := 3
		var ids []int64
		if option.concurrency != nil {
			concurrency = *option.concurrency
		}
		if option.ids != nil {
			ids = *option.ids
		}
		if err := config.ValidateCodexTicketHarvestOptions(ids, concurrency); err != nil {
			return err
		}
	}
	return nil
}
