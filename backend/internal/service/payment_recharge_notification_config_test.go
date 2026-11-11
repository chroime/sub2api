//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testPaymentAdminRechargeNotifyEnabledKey = "PAYMENT_ADMIN_RECHARGE_NOTIFY_ENABLED"
	testPaymentAdminRechargeNotifyEmailsKey  = "PAYMENT_ADMIN_RECHARGE_NOTIFY_EMAILS"
)

type paymentAdminRechargeConfigRepo struct {
	paymentConfigSettingRepoStub
	reads   [][]string
	writes  int
	readErr error
}

func (r *paymentAdminRechargeConfigRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.reads = append(r.reads, append([]string(nil), keys...))
	if r.readErr != nil {
		return nil, r.readErr
	}
	return r.paymentConfigSettingRepoStub.GetMultiple(ctx, keys)
}

func (r *paymentAdminRechargeConfigRepo) SetMultiple(ctx context.Context, values map[string]string) error {
	r.writes++
	return r.paymentConfigSettingRepoStub.SetMultiple(ctx, values)
}

func adminRechargeConfigRequest(t *testing.T, values map[string]any) UpdatePaymentConfigRequest {
	t.Helper()
	raw, err := json.Marshal(values)
	require.NoError(t, err)
	var req UpdatePaymentConfigRequest
	require.NoError(t, json.Unmarshal(raw, &req))
	return req
}

func adminRechargeConfigJSON(t *testing.T, cfg *PaymentConfig) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &data))
	return data
}

func TestPaymentAdminRechargeNotificationConfigDefaultsAndAdminRoundTrip(t *testing.T) {
	repo := &paymentAdminRechargeConfigRepo{}
	svc := NewPaymentConfigService(nil, repo, nil)
	cfg, err := svc.GetAdminPaymentConfig(t.Context())
	require.NoError(t, err)
	data := adminRechargeConfigJSON(t, cfg)
	require.JSONEq(t, "false", string(data["admin_recharge_notify_enabled"]))
	require.JSONEq(t, "[]", string(data["admin_recharge_notify_emails"]))

	err = svc.UpdatePaymentConfig(t.Context(), adminRechargeConfigRequest(t, map[string]any{
		"admin_recharge_notify_enabled": true,
		"admin_recharge_notify_emails":  []string{" Admin@Example.Test ", "SECOND@example.test", "admin@example.test"},
	}))
	require.NoError(t, err)
	require.Equal(t, "true", repo.values[testPaymentAdminRechargeNotifyEnabledKey])
	require.JSONEq(t, `["admin@example.test","second@example.test"]`, repo.values[testPaymentAdminRechargeNotifyEmailsKey])
	cfg, err = svc.GetAdminPaymentConfig(t.Context())
	require.NoError(t, err)
	data = adminRechargeConfigJSON(t, cfg)
	require.JSONEq(t, "true", string(data["admin_recharge_notify_enabled"]))
	require.JSONEq(t, `["admin@example.test","second@example.test"]`, string(data["admin_recharge_notify_emails"]))
}

func TestPaymentAdminRechargeNotificationConfigReadsOnlyIndependentKeys(t *testing.T) {
	repo := &paymentAdminRechargeConfigRepo{paymentConfigSettingRepoStub: paymentConfigSettingRepoStub{values: map[string]string{
		testPaymentAdminRechargeNotifyEnabledKey: "true",
		testPaymentAdminRechargeNotifyEmailsKey:  `[" Admin@Example.Test ","admin@example.test"]`,
		SettingKeyAccountQuotaNotifyEmails:       `["quota@example.test"]`,
	}}}
	svc := NewPaymentConfigService(nil, repo, nil)
	reader, ok := any(svc).(interface {
		GetAdminRechargeNotificationConfig(context.Context) (bool, []string, error)
	})
	require.True(t, ok, "the payment notifier needs a narrow settings reader without provider queries")
	enabled, emails, err := reader.GetAdminRechargeNotificationConfig(t.Context())
	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, []string{"admin@example.test"}, emails)
	require.Len(t, repo.reads, 1)
	require.ElementsMatch(t, []string{testPaymentAdminRechargeNotifyEnabledKey, testPaymentAdminRechargeNotifyEmailsKey}, repo.reads[0])
	emails[0] = "changed@example.test"
	_, again, err := reader.GetAdminRechargeNotificationConfig(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"admin@example.test"}, again)
}

func TestPaymentAdminRechargeNotificationConfigStoredDataFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		readErr error
	}{
		{name: "malformed json", raw: `["admin@example.test"`},
		{name: "wrong json type", raw: `"admin@example.test"`},
		{name: "invalid stored mailbox", raw: `["Admin <admin@example.test>"]`},
		{name: "enabled without recipient", raw: `[]`},
		{name: "repository unavailable", readErr: errors.New("settings unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &paymentAdminRechargeConfigRepo{paymentConfigSettingRepoStub: paymentConfigSettingRepoStub{values: map[string]string{
				testPaymentAdminRechargeNotifyEnabledKey: "true",
				testPaymentAdminRechargeNotifyEmailsKey:  tc.raw,
			}}, readErr: tc.readErr}
			reader, ok := any(NewPaymentConfigService(nil, repo, nil)).(interface {
				GetAdminRechargeNotificationConfig(context.Context) (bool, []string, error)
			})
			require.True(t, ok)
			enabled, emails, err := reader.GetAdminRechargeNotificationConfig(t.Context())
			require.Error(t, err)
			require.False(t, enabled)
			require.Empty(t, emails)
		})
	}
}

func TestPaymentAdminRechargeNotificationConfigPatchPreservesOmittedFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		patch  map[string]any
		stored map[string]string
		want   map[string]string
	}{
		{name: "unrelated payment patch", patch: map[string]any{"enabled": true}, stored: map[string]string{testPaymentAdminRechargeNotifyEnabledKey: "true", testPaymentAdminRechargeNotifyEmailsKey: `["admin@example.test"]`}, want: map[string]string{SettingPaymentEnabled: "true"}},
		{name: "enable saved draft recipients", patch: map[string]any{"admin_recharge_notify_enabled": true}, stored: map[string]string{testPaymentAdminRechargeNotifyEnabledKey: "false", testPaymentAdminRechargeNotifyEmailsKey: `["admin@example.test"]`}, want: map[string]string{testPaymentAdminRechargeNotifyEnabledKey: "true"}},
		{name: "change enabled recipients only", patch: map[string]any{"admin_recharge_notify_emails": []string{" New@Example.Test "}}, stored: map[string]string{testPaymentAdminRechargeNotifyEnabledKey: "true", testPaymentAdminRechargeNotifyEmailsKey: `["admin@example.test"]`}, want: map[string]string{testPaymentAdminRechargeNotifyEmailsKey: `["new@example.test"]`}},
		{name: "disable without clearing draft", patch: map[string]any{"admin_recharge_notify_enabled": false}, stored: map[string]string{testPaymentAdminRechargeNotifyEnabledKey: "true", testPaymentAdminRechargeNotifyEmailsKey: `["admin@example.test"]`}, want: map[string]string{testPaymentAdminRechargeNotifyEnabledKey: "false"}},
		{name: "disable and clear draft", patch: map[string]any{"admin_recharge_notify_enabled": false, "admin_recharge_notify_emails": []string{}}, stored: map[string]string{testPaymentAdminRechargeNotifyEnabledKey: "true", testPaymentAdminRechargeNotifyEmailsKey: `["admin@example.test"]`}, want: map[string]string{testPaymentAdminRechargeNotifyEnabledKey: "false", testPaymentAdminRechargeNotifyEmailsKey: `[]`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &paymentAdminRechargeConfigRepo{paymentConfigSettingRepoStub: paymentConfigSettingRepoStub{values: map[string]string{}}}
			for key, value := range tc.stored {
				repo.values[key] = value
			}
			svc := NewPaymentConfigService(nil, repo, nil)
			require.NoError(t, svc.UpdatePaymentConfig(t.Context(), adminRechargeConfigRequest(t, tc.patch)))
			for key, value := range tc.want {
				require.Equal(t, value, repo.values[key], key)
			}
			for key, value := range tc.stored {
				if _, updated := tc.want[key]; !updated {
					require.Equal(t, value, repo.values[key], "omitted field must be preserved")
					_, written := repo.updates[key]
					require.False(t, written, "omitted field must not be rewritten")
				}
			}
		})
	}
}

func TestPaymentAdminRechargeNotificationConfigRejectsInvalidBeforeAnyWrite(t *testing.T) {
	eleven := make([]string, 11)
	for i := range eleven {
		eleven[i] = fmt.Sprintf("admin%d@example.test", i)
	}
	for _, tc := range []struct {
		name       string
		patch      map[string]any
		wasEnabled bool
	}{
		{name: "enable without recipients", patch: map[string]any{"admin_recharge_notify_enabled": true}},
		{name: "clear while enabled", patch: map[string]any{"admin_recharge_notify_emails": []string{}}, wasEnabled: true},
		{name: "display name", patch: map[string]any{"admin_recharge_notify_emails": []string{"Admin <admin@example.test>"}}},
		{name: "angle address", patch: map[string]any{"admin_recharge_notify_emails": []string{"<admin@example.test>"}}},
		{name: "header injection", patch: map[string]any{"admin_recharge_notify_emails": []string{"admin@example.test\r\nBcc: victim@example.test"}}},
		{name: "line break suffix", patch: map[string]any{"admin_recharge_notify_emails": []string{"admin@example.test\n"}}},
		{name: "invalid mailbox", patch: map[string]any{"admin_recharge_notify_emails": []string{"not-an-email"}}},
		{name: "blank entry", patch: map[string]any{"admin_recharge_notify_emails": []string{" "}}},
		{name: "eleven recipients", patch: map[string]any{"admin_recharge_notify_emails": eleven}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial := map[string]string{SettingHelpText: "keep help", testPaymentAdminRechargeNotifyEnabledKey: "false", testPaymentAdminRechargeNotifyEmailsKey: `[]`}
			if tc.wasEnabled {
				initial[testPaymentAdminRechargeNotifyEnabledKey] = "true"
				initial[testPaymentAdminRechargeNotifyEmailsKey] = `["admin@example.test"]`
			}
			repo := &paymentAdminRechargeConfigRepo{paymentConfigSettingRepoStub: paymentConfigSettingRepoStub{values: map[string]string{}}}
			for key, value := range initial {
				repo.values[key] = value
			}
			tc.patch["help_text"] = "must not persist"
			err := NewPaymentConfigService(nil, repo, nil).UpdatePaymentConfig(t.Context(), adminRechargeConfigRequest(t, tc.patch))
			require.Error(t, err)
			require.Zero(t, repo.writes, "invalid notification settings must reject the complete update before SetMultiple")
			require.Equal(t, initial, repo.values)
		})
	}
}
