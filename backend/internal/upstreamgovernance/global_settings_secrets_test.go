package upstreamgovernance_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

type globalSecretStore struct {
	raw               string
	readErr, writeErr error
}

func (s *globalSecretStore) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{gov.GlobalSettingsKey: s.raw}, s.readErr
}

func (s *globalSecretStore) SetMultiple(_ context.Context, values map[string]string) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.raw = values[gov.GlobalSettingsKey]
	return nil
}

func globalSecretService(t *testing.T, store *globalSecretStore, keyByte string) *gov.Service {
	t.Helper()
	cipher, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat(keyByte, 32)}})
	require.NoError(t, err)
	svc := gov.NewService(nil, nil, nil, cipher, true)
	svc.SetGlobalSettingsStore(store)
	return svc
}

func storedGlobalSecret(t *testing.T, store *globalSecretStore) string {
	t.Helper()
	var stored struct {
		AstrBot struct {
			APIKey      string `json:"api_key"`
			Ciphertext  string `json:"api_key_ciphertext"`
			ClearAPIKey bool   `json:"clear_api_key"`
		} `json:"astrbot"`
	}
	require.NoError(t, json.Unmarshal([]byte(store.raw), &stored))
	require.Empty(t, stored.AstrBot.APIKey)
	require.False(t, stored.AstrBot.ClearAPIKey)
	return stored.AstrBot.Ciphertext
}

func TestGlobalSettingsSecretsEncryptAndSurviveRestart(t *testing.T) {
	store := &globalSecretStore{}
	svc := globalSecretService(t, store, "42")
	settings, err := svc.GetGlobalSettings(t.Context())
	require.NoError(t, err)
	require.False(t, settings.AstrBot.APIKeyConfigured)
	settings.AstrBot.APIKey = "astrbot-persistent-secret"
	updated, err := svc.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Version)
	require.Empty(t, updated.AstrBot.APIKey)
	require.True(t, updated.AstrBot.APIKeyConfigured)
	require.NotContains(t, store.raw, "astrbot-persistent-secret")
	ciphertext := storedGlobalSecret(t, store)
	require.NotEmpty(t, ciphertext)

	restarted := globalSecretService(t, store, "42")
	runtime, err := restarted.GetRuntimeGlobalSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "astrbot-persistent-secret", runtime.AstrBot.APIKey)
	metadata, err := restarted.GetGlobalSettings(t.Context())
	require.NoError(t, err)
	public, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.Contains(t, string(public), `"api_key":""`)
	require.NotContains(t, string(public), "api_key_ciphertext")
	require.NotContains(t, string(public), ciphertext)
	require.NotContains(t, string(public), "astrbot-persistent-secret")
}

func TestGlobalSettingsSecretsPreserveReplaceAndClear(t *testing.T) {
	store := &globalSecretStore{}
	svc := globalSecretService(t, store, "42")
	settings, err := svc.GetGlobalSettings(t.Context())
	require.NoError(t, err)
	settings.AstrBot.APIKey = "first-astrbot-secret"
	settings, err = svc.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	ciphertext := storedGlobalSecret(t, store)

	// Requests from an older client can omit both the key and status flag.
	var omitted gov.GlobalSettings
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"astrbot":{"base_url":"https://bot.example.test","api_key_configured":false}}`), &omitted))
	settings, err = svc.SaveGlobalSettings(t.Context(), omitted)
	require.NoError(t, err)
	require.True(t, settings.AstrBot.APIKeyConfigured)
	require.Equal(t, ciphertext, storedGlobalSecret(t, store))
	require.Equal(t, "https://bot.example.test", settings.AstrBot.BaseURL)

	settings.AstrBot.APIKey = ""
	settings, err = svc.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	require.Equal(t, ciphertext, storedGlobalSecret(t, store))
	runtime, err := svc.GetRuntimeGlobalSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "first-astrbot-secret", runtime.AstrBot.APIKey)

	settings.AstrBot.APIKey = "replacement-astrbot-secret"
	settings, err = svc.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	require.NotEqual(t, ciphertext, storedGlobalSecret(t, store))
	require.NotContains(t, store.raw, "replacement-astrbot-secret")
	runtime, err = svc.GetRuntimeGlobalSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "replacement-astrbot-secret", runtime.AstrBot.APIKey)

	settings.AstrBot.ClearAPIKey = true
	settings, err = svc.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	require.Empty(t, storedGlobalSecret(t, store))
	require.False(t, settings.AstrBot.APIKeyConfigured)
	require.False(t, settings.AstrBot.ClearAPIKey)
	runtime, err = svc.GetRuntimeGlobalSettings(t.Context())
	require.NoError(t, err)
	require.Empty(t, runtime.AstrBot.APIKey)
}

func TestGlobalSettingsSecretsMigrateLegacyOnSave(t *testing.T) {
	store := &globalSecretStore{raw: `{"version":7,"astrbot":{"api_key":" legacy-astrbot-secret ","api_key_configured":false}}`}
	svc := globalSecretService(t, store, "42")
	runtime, err := svc.GetRuntimeGlobalSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, " legacy-astrbot-secret ", runtime.AstrBot.APIKey)
	require.True(t, runtime.AstrBot.APIKeyConfigured)
	settings, err := svc.GetGlobalSettings(t.Context())
	require.NoError(t, err)
	settings.AstrBot.APIKey = "" // A redacted edit preserves and migrates the key.
	settings.LocalMonitor.Enabled = true
	settings, err = svc.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	require.NotContains(t, store.raw, "legacy-astrbot-secret")
	require.NotEmpty(t, storedGlobalSecret(t, store))
	require.EqualValues(t, 8, settings.Version)
	runtime, err = globalSecretService(t, store, "42").GetRuntimeGlobalSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, " legacy-astrbot-secret ", runtime.AstrBot.APIKey)
	require.True(t, runtime.LocalMonitor.Enabled)
}

func TestGlobalSettingsSecretsRejectCiphertextInput(t *testing.T) {
	for _, ciphertext := range []string{`"forged-secret"`, `null`, `{}`} {
		t.Run(ciphertext, func(t *testing.T) {
			store := &globalSecretStore{}
			svc := globalSecretService(t, store, "42")
			var input gov.GlobalSettings
			require.NoError(t, json.Unmarshal([]byte(`{"version":1,"astrbot":{"api_key":"new-secret","api_key_ciphertext":`+ciphertext+`}}`), &input))
			_, err := svc.SaveGlobalSettings(t.Context(), input)
			require.ErrorIs(t, err, gov.ErrInvalid)
			require.Empty(t, store.raw)
		})
	}
}

func TestGlobalSettingsSecretsRequireDurableEncryption(t *testing.T) {
	for _, existing := range []string{"", `{"version":1,"astrbot":{"api_key":"legacy-astrbot-secret"}}`} {
		for _, durable := range []bool{false, true} {
			store := &globalSecretStore{raw: existing}
			svc := gov.NewService(nil, nil, nil, nil, durable)
			svc.SetGlobalSettingsStore(store)
			settings, err := svc.GetGlobalSettings(t.Context())
			require.NoError(t, err)
			if existing == "" {
				settings.AstrBot.APIKey = "new-astrbot-secret"
			} else {
				runtime, err := svc.GetRuntimeGlobalSettings(t.Context())
				require.NoError(t, err)
				require.Equal(t, "legacy-astrbot-secret", runtime.AstrBot.APIKey)
				settings.AstrBot.APIKey = ""
			}
			_, err = svc.SaveGlobalSettings(t.Context(), settings)
			require.ErrorIs(t, err, gov.ErrEncryption)
			require.Equal(t, existing, store.raw)
			settings.AstrBot.ClearAPIKey = true
			_, err = svc.SaveGlobalSettings(t.Context(), settings)
			require.NoError(t, err)
			require.Empty(t, storedGlobalSecret(t, store))
		}
	}
	store := &globalSecretStore{}
	cipher, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	svc := gov.NewService(nil, nil, nil, cipher, false)
	svc.SetGlobalSettingsStore(store)
	_, err = svc.SaveGlobalSettings(t.Context(), gov.GlobalSettings{Version: 1, AstrBot: gov.AstrBotSettings{APIKey: "secret"}})
	require.ErrorIs(t, err, gov.ErrEncryption)
	require.Empty(t, store.raw)
}

func TestGlobalSettingsSecretsCorruptionDoesNotBlockPolicyOrRecovery(t *testing.T) {
	store := &globalSecretStore{raw: `{"version":1,"notifications":{"enabled":true,"recipients":["ops@example.test"]},"astrbot":{"api_key":"legacy-fallback-must-not-be-used","api_key_ciphertext":"invalid-encrypted-secret"}}`}
	svc := globalSecretService(t, store, "42")
	settings, err := svc.GetGlobalSettings(t.Context())
	require.NoError(t, err)
	require.True(t, settings.Notifications.Enabled)
	require.True(t, settings.AstrBot.APIKeyConfigured)
	require.Empty(t, settings.AstrBot.APIKey)
	_, err = svc.GetRuntimeGlobalSettings(t.Context())
	require.ErrorIs(t, err, gov.ErrEncryption)
	require.NotContains(t, err.Error(), "invalid-encrypted-secret")

	settings.LocalMonitor.Enabled = true
	settings, err = svc.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	require.Equal(t, "invalid-encrypted-secret", storedGlobalSecret(t, store))
	settings.AstrBot.APIKey = "recovered-astrbot-secret"
	settings, err = svc.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	runtime, err := svc.GetRuntimeGlobalSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "recovered-astrbot-secret", runtime.AstrBot.APIKey)
	_, err = globalSecretService(t, store, "43").GetRuntimeGlobalSettings(t.Context())
	require.ErrorIs(t, err, gov.ErrEncryption)

	withoutCipher := gov.NewService(nil, nil, nil, nil, false)
	withoutCipher.SetGlobalSettingsStore(store)
	_, err = withoutCipher.GetRuntimeGlobalSettings(t.Context())
	require.ErrorIs(t, err, gov.ErrEncryption)
	settings.AstrBot.ClearAPIKey = true
	_, err = withoutCipher.SaveGlobalSettings(t.Context(), settings)
	require.NoError(t, err)
	require.Empty(t, storedGlobalSecret(t, store))
}

type failingGlobalSecretCipher struct{}

func (failingGlobalSecretCipher) Encrypt(value string) (string, error) {
	return "", errors.New("encrypt failed: " + value)
}

func (failingGlobalSecretCipher) Decrypt(value string) (string, error) {
	return "", errors.New("decrypt failed: " + value)
}

func TestGlobalSettingsSecretsCipherAndStoreFailures(t *testing.T) {
	store := &globalSecretStore{}
	svc := gov.NewService(nil, nil, nil, failingGlobalSecretCipher{}, true)
	svc.SetGlobalSettingsStore(store)
	input := gov.GlobalSettings{Version: 1, AstrBot: gov.AstrBotSettings{APIKey: "sensitive-test-secret"}}
	_, err := svc.SaveGlobalSettings(t.Context(), input)
	require.ErrorIs(t, err, gov.ErrEncryption)
	require.NotContains(t, err.Error(), input.AstrBot.APIKey)
	require.Empty(t, store.raw)
	store.raw = `{"version":1,"astrbot":{"api_key_ciphertext":"sensitive-ciphertext"}}`
	_, err = svc.GetRuntimeGlobalSettings(t.Context())
	require.ErrorIs(t, err, gov.ErrEncryption)
	require.NotContains(t, err.Error(), "sensitive-ciphertext")

	store.raw = ""
	store.readErr = errors.New("settings read unavailable")
	_, err = svc.GetGlobalSettings(t.Context())
	require.ErrorIs(t, err, store.readErr)
	_, err = svc.SaveGlobalSettings(t.Context(), input)
	require.ErrorIs(t, err, store.readErr)
	store.readErr = nil
	store.writeErr = errors.New("settings write unavailable")
	_, err = globalSecretService(t, store, "42").SaveGlobalSettings(t.Context(), input)
	require.ErrorIs(t, err, store.writeErr)
	require.Empty(t, store.raw)
}
