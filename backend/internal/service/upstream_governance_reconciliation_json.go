package service

import "encoding/json"

func decodeGovernanceValue(value any, target any) {
	raw, err := json.Marshal(value)
	if err == nil {
		_ = json.Unmarshal(raw, target)
	}
}
