package upstreamgovernance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGlobalSettingsMigratesGranularEventsIndependently(t *testing.T) {
	tests := []struct {
		name, raw string
		mail, qq  []bool
	}{
		{"legacy enabled", `{"notifications":{"events":{"upstream_group_change":true,"upstream_rate_change":true}},"astrbot":{"events":{"local_group_change":true}}}`, []bool{true, true, true, true, true}, []bool{true, true, true, true, true}},
		{"legacy disabled", `{"notifications":{"events":{"upstream_group_change":false,"upstream_rate_change":false}},"astrbot":{"events":{"local_group_change":false}}}`, []bool{false, false, false, false, false}, []bool{false, false, false, false, false}},
		{"rate only mail", `{"notifications":{"events":{"upstream_group_change":false,"upstream_rate_change":true}},"astrbot":{"events":{"local_group_change":false}}}`, []bool{false, false, false, false, true}, []bool{false, false, false, false, false}},
		{"explicit values win", `{"notifications":{"events":{"upstream_group_change":true,"group_online":false,"group_rate_change":false}},"astrbot":{"events":{"local_group_change":false,"group_name_change":true}}}`, []bool{false, true, true, true, false}, []bool{false, false, true, false, false}},
		{"defaults", `{}`, []bool{true, true, true, true, true}, []bool{true, true, true, true, true}},
	}
	keys := []string{"group_online", "group_offline", "group_name_change", "group_description_change", "group_rate_change"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings, err := ParseGlobalSettings(tt.raw)
			require.NoError(t, err)
			for channel, events := range map[string]GlobalNotificationEvents{"mail": settings.Notifications.Events, "qq": settings.AstrBot.Events} {
				raw, err := json.Marshal(events)
				require.NoError(t, err)
				var fields map[string]bool
				require.NoError(t, json.Unmarshal(raw, &fields))
				want := tt.mail
				if channel == "qq" {
					want = tt.qq
				}
				for i, key := range keys {
					require.Contains(t, fields, key, channel)
					require.Equal(t, want[i], fields[key], "%s.%s", channel, key)
				}
			}
			encoded, err := json.Marshal(settings)
			require.NoError(t, err)
			roundtrip, err := ParseGlobalSettings(string(encoded))
			require.NoError(t, err)
			require.Equal(t, settings, roundtrip)
		})
	}
}

func TestGlobalSettingsLegacyAPIRequestMigratesWithoutResettingDestinations(t *testing.T) {
	raw := `{"version":12,"notifications":{"enabled":true,"recipients":["ops@example.test"],"events":{"upstream_group_change":false,"upstream_rate_change":true}},"local_monitor":{"enabled":true,"interval_seconds":3},"astrbot":{"enabled":true,"base_url":"https://bot.example.test","endpoint_path":"/api/v1/im/message","api_key":"test-key","timeout_seconds":100,"events":{"local_group_change":false,"group_description_change":true},"targets":[{"umo":"bot:GroupMessage:12","label":"Ops","enabled":true}]}}`
	var input GlobalSettings
	require.NoError(t, json.Unmarshal([]byte(raw), &input))
	parsed, err := ParseGlobalSettings(raw)
	require.NoError(t, err)
	require.Equal(t, parsed.Notifications.Recipients, input.Notifications.Recipients)
	require.Equal(t, parsed.AstrBot.APIKey, input.AstrBot.APIKey)
	require.EqualValues(t, 12, input.Version)
	require.Equal(t, []string{"ops@example.test"}, input.Notifications.Recipients)
	require.Equal(t, "test-key", input.AstrBot.APIKey)
	require.EqualValues(t, 3, input.LocalMonitor.IntervalSeconds)
	require.EqualValues(t, 100, input.AstrBot.TimeoutSeconds)
	require.Equal(t, "bot:GroupMessage:12", input.AstrBot.Targets[0].UMO)
}

func TestGlobalSettingsRoundTripUsesGlobalStore(t *testing.T) {
	store := &globalSettingsMemoryStore{}
	svc := NewService(nil, nil, nil, nil, false)
	svc.SetGlobalSettingsStore(store)

	initial, err := svc.GetGlobalSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if initial.Version != 1 {
		t.Fatalf("default version = %d, want 1", initial.Version)
	}
	if initial.Notifications.Events.LocalGroupChange {
		t.Fatal("email notifications should not enable local-group changes by default")
	}
	if !initial.AstrBot.Events.LocalGroupChange {
		t.Fatal("AstrBot notifications should enable local-group changes by default")
	}

	initial.Notifications.Enabled = true
	initial.Notifications.Recipients = []string{"ops@example.com"}
	initial.LocalMonitor.Enabled = true
	initial.LocalMonitor.IntervalSeconds = 17
	updated, err := svc.SaveGlobalSettings(context.Background(), initial)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatalf("saved version = %d, want 2", updated.Version)
	}

	got, err := svc.GetGlobalSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.LocalMonitor.IntervalSeconds != 17 || len(got.AstrBot.Targets) != 0 {
		t.Fatalf("round trip lost values: %#v", got)
	}
}

func TestGlobalSettingsNormalizeDisablesLegacyMailLocalGroupEvent(t *testing.T) {
	raw := `{"version":1,"notifications":{"enabled":true,"recipients":["ops@example.com"],"events":{"upstream_group_change":true,"upstream_rate_change":true,"pricing_change":true,"protection_change":true,"local_group_change":true,"health_change":true}},"astrbot":{"events":{"local_group_change":true}}}`
	settings, err := ParseGlobalSettings(raw)
	require.NoError(t, err)
	require.False(t, settings.Notifications.Events.LocalGroupChange)
	require.True(t, settings.AstrBot.Events.LocalGroupChange)
}

func TestGlobalNotificationAllowsNeverRoutesLocalGroupChangeToMail(t *testing.T) {
	events := GlobalNotificationEvents{LocalGroupChange: true}
	require.False(t, globalNotificationAllows(events, "local_group_change"))
}

func TestGlobalSettingsLegacyAstrBotConfigGetsIndependentLocalEvents(t *testing.T) {
	settings, err := ParseGlobalSettings(`{"notifications":{"enabled":false,"events":{"local_group_change":true}},"astrbot":{"enabled":true}}`)
	require.NoError(t, err)
	require.False(t, settings.Notifications.Events.LocalGroupChange)
	require.True(t, settings.AstrBot.Events.LocalGroupChange)
	require.False(t, settings.AstrBot.Events.UpstreamGroupChange)
	require.False(t, settings.AstrBot.Events.UpstreamRateChange)
}

func TestGlobalSettingsKeepsExplicitAstrBotLocalEventDisabled(t *testing.T) {
	settings, err := ParseGlobalSettings(`{"notifications":{"enabled":true,"events":{"local_group_change":true}},"astrbot":{"enabled":true,"events":{"local_group_change":false}}}`)
	require.NoError(t, err)
	require.False(t, settings.Notifications.Events.LocalGroupChange)
	require.False(t, settings.AstrBot.Events.LocalGroupChange)
}

type globalSettingsMemoryStore struct{ values map[string]string }

func (s *globalSettingsMemoryStore) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := map[string]string{}
	for _, key := range keys {
		if s.values != nil {
			if value, ok := s.values[key]; ok {
				result[key] = value
			}
		}
	}
	return result, nil
}

func (s *globalSettingsMemoryStore) SetMultiple(_ context.Context, values map[string]string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}
