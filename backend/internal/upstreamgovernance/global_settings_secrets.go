package upstreamgovernance

import (
	"context"
	"encoding/json"
	"strings"
)

// Ciphertext has a storage-only representation. The public settings model
// cannot serialize it, and API input cannot replace the stored ciphertext.
func parseGlobalSettingsSecrets(raw string, value *GlobalSettings) error {
	var stored struct {
		AstrBot struct {
			APIKeyCiphertext string `json:"api_key_ciphertext"`
		} `json:"astrbot"`
	}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return err
	}
	value.AstrBot.apiKeyCiphertext = stored.AstrBot.APIKeyCiphertext
	if value.AstrBot.apiKeyCiphertext != "" {
		// Prefer the encrypted value if an old or manually edited record has
		// both representations. Never fall back after a decryption failure.
		value.AstrBot.APIKey = ""
	}
	value.AstrBot.APIKeyConfigured = strings.TrimSpace(value.AstrBot.APIKey) != "" || value.AstrBot.apiKeyCiphertext != ""
	value.AstrBot.ClearAPIKey = false
	value.AstrBot.ciphertextProvided = false
	return nil
}

func marshalGlobalSettings(value GlobalSettings) ([]byte, error) {
	value.AstrBot.APIKey = ""
	value.AstrBot.ClearAPIKey = false
	return json.Marshal(struct {
		GlobalSettings
		AstrBot struct {
			AstrBotSettings
			APIKeyCiphertext string `json:"api_key_ciphertext,omitempty"`
		} `json:"astrbot"`
	}{
		GlobalSettings: value,
		AstrBot: struct {
			AstrBotSettings
			APIKeyCiphertext string `json:"api_key_ciphertext,omitempty"`
		}{value.AstrBot, value.AstrBot.apiKeyCiphertext},
	})
}

func (s *Service) prepareGlobalSettingsSecrets(input, current GlobalSettings) (GlobalSettings, error) {
	key := input.AstrBot.APIKey
	if strings.TrimSpace(key) == "" {
		key = ""
	}
	input.AstrBot.apiKeyCiphertext = ""
	if !input.AstrBot.ClearAPIKey {
		if key == "" {
			input.AstrBot.apiKeyCiphertext = current.AstrBot.apiKeyCiphertext
			if input.AstrBot.apiKeyCiphertext == "" {
				key = current.AstrBot.APIKey
			}
		}
		if strings.TrimSpace(key) != "" {
			if !s.durableKey || s.cipher == nil {
				return GlobalSettings{}, ErrEncryption
			}
			encrypted, err := s.cipher.Encrypt(key)
			if err != nil || encrypted == "" {
				// Encryptor errors may contain their input; keep credentials out
				// of logs and HTTP error responses.
				return GlobalSettings{}, ErrEncryption
			}
			input.AstrBot.apiKeyCiphertext = encrypted
		}
	}
	input.AstrBot.APIKey = ""
	input.AstrBot.APIKeyConfigured = input.AstrBot.apiKeyCiphertext != ""
	input.AstrBot.ClearAPIKey = false
	return input, nil
}

// GetRuntimeGlobalSettings resolves credentials only for consumers that need
// to authenticate an AstrBot request. Policy reads use GetGlobalSettings so a
// damaged credential cannot block unrelated mail or scheduling decisions.
func (s *Service) GetRuntimeGlobalSettings(ctx context.Context) (GlobalSettings, error) {
	value, err := s.GetGlobalSettings(ctx)
	if err != nil {
		return GlobalSettings{}, err
	}
	if value.AstrBot.apiKeyCiphertext == "" {
		return value, nil // Legacy plaintext remains usable until the next save.
	}
	if !s.durableKey || s.cipher == nil {
		return GlobalSettings{}, ErrEncryption
	}
	key, err := s.cipher.Decrypt(value.AstrBot.apiKeyCiphertext)
	if err != nil || strings.TrimSpace(key) == "" {
		return GlobalSettings{}, ErrEncryption
	}
	value.AstrBot.APIKey = key
	return value, nil
}
