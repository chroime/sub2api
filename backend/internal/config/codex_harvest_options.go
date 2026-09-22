package config

import "errors"

func ValidateCodexTicketHarvestOptions(proxyIDs []int64, concurrency int) error {
	if concurrency < 1 || concurrency > 16 {
		return errors.New("harvest_concurrency must be between 1 and 16")
	}
	if len(proxyIDs) > 32 {
		return errors.New("harvest_proxy_ids supports at most 32 proxies")
	}
	seen := make(map[int64]struct{}, len(proxyIDs))
	for _, id := range proxyIDs {
		if id <= 0 {
			return errors.New("harvest_proxy_ids must contain positive IDs")
		}
		if _, duplicate := seen[id]; duplicate {
			return errors.New("harvest_proxy_ids must contain unique IDs")
		}
		seen[id] = struct{}{}
	}
	return nil
}
