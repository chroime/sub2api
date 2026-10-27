package service

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

type governanceNotificationSecretCipher struct{}

func (governanceNotificationSecretCipher) Encrypt(value string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(value)), nil
}

func (governanceNotificationSecretCipher) Decrypt(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	return string(decoded), err
}

func TestGovernanceAstrBotNotifierResolvesEncryptedKeyAtDelivery(t *testing.T) {
	var authorization string
	var received struct {
		UMO     string `json:"umo"`
		Message []struct {
			Text string `json:"text"`
		} `json:"message"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	repo := newNotificationEmailMemorySettingRepo()
	svc := gov.NewService(nil, nil, nil, governanceNotificationSecretCipher{}, true)
	svc.SetGlobalSettingsStore(repo)
	_, err := svc.SaveGlobalSettings(t.Context(), gov.GlobalSettings{
		Version: 1,
		AstrBot: gov.AstrBotSettings{
			Enabled: true, BaseURL: server.URL, APIKey: "runtime-notification-secret",
			Targets: []gov.AstrBotTarget{{UMO: "bot:GroupMessage:42", Enabled: true}},
		},
	})
	require.NoError(t, err)
	stored, err := repo.GetValue(t.Context(), gov.GlobalSettingsKey)
	require.NoError(t, err)
	require.NotContains(t, stored, "runtime-notification-secret")
	adapter := &governanceBalanceNotifier{settings: repo, runtimeGlobalSettings: svc.GetRuntimeGlobalSettings}
	recipients, err := adapter.AstrBotRecipients(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"qq:bot:GroupMessage:42"}, recipients)
	notice := gov.ChangeNotice{Subject: "Group change", Body: "Email details", AstrBotText: "Selected QQ event details"}
	require.NoError(t, adapter.SendChange(t.Context(), recipients[0], notice))
	require.Equal(t, "Bearer runtime-notification-secret", authorization)
	require.Equal(t, "bot:GroupMessage:42", received.UMO)
	require.Len(t, received.Message, 1)
	require.Equal(t, "Selected QQ event details", received.Message[0].Text)
}

func TestGovernanceNotificationPolicyRemainsReadableWhenAstrBotKeyCannotDecrypt(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.Set(t.Context(), gov.GlobalSettingsKey, `{"version":1,"notifications":{"enabled":true,"recipients":["ops@example.test"]},"astrbot":{"enabled":true,"base_url":"https://bot.example.test","api_key_ciphertext":"invalid-ciphertext","targets":[{"umo":"bot:GroupMessage:42","enabled":true}]}}`))
	svc := gov.NewService(nil, nil, nil, governanceNotificationSecretCipher{}, true)
	svc.SetGlobalSettingsStore(repo)
	adapter := &governanceBalanceNotifier{settings: repo, runtimeGlobalSettings: svc.GetRuntimeGlobalSettings}
	emailRecipients, err := adapter.Recipients(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, []string{"ops@example.test"}, emailRecipients)
	qqRecipients, err := adapter.AstrBotRecipients(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"qq:bot:GroupMessage:42"}, qqRecipients)
	err = adapter.SendChange(t.Context(), qqRecipients[0], gov.ChangeNotice{})
	require.ErrorIs(t, err, gov.ErrEncryption)
	require.NotContains(t, err.Error(), "invalid-ciphertext")
}
