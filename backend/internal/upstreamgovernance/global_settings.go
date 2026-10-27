package upstreamgovernance

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// GlobalSettingsStore is the small portion of the application's settings
// repository needed by governance. Keeping this interface in the governance
// package avoids a dependency cycle with internal/service while allowing the
// normal settings table to be used in production.
type GlobalSettingsStore interface {
	GetMultiple(context.Context, []string) (map[string]string, error)
	SetMultiple(context.Context, map[string]string) error
}

const globalSettingsKey = "upstream_governance_global_settings"

// GlobalSettingsKey is the settings-table key used by governance adapters
// that need to observe the same global notification policy.
const GlobalSettingsKey = globalSettingsKey

const maxGlobalCadenceSeconds int64 = 128849018820

// GlobalNotificationEvents controls which governance events are eligible for
// the global notification fan-out. Event names intentionally mirror the
// frontend contract and the site-level pricing notification vocabulary.
type GlobalNotificationEvents struct {
	GroupOnline            bool `json:"group_online"`
	GroupOffline           bool `json:"group_offline"`
	GroupNameChange        bool `json:"group_name_change"`
	GroupDescriptionChange bool `json:"group_description_change"`
	GroupRateChange        bool `json:"group_rate_change"`
	// Legacy fields are retained only to migrate persisted and older API input.
	UpstreamGroupChange bool `json:"upstream_group_change"`
	UpstreamRateChange  bool `json:"upstream_rate_change"`
	PricingChange       bool `json:"pricing_change"`
	ProtectionChange    bool `json:"protection_change"`
	LocalGroupChange    bool `json:"local_group_change"`
	HealthChange        bool `json:"health_change"`
}

type GlobalNotificationSettings struct {
	Enabled    bool                     `json:"enabled"`
	Recipients []string                 `json:"recipients"`
	Events     GlobalNotificationEvents `json:"events"`
}

func globalNotificationAllows(events GlobalNotificationEvents, kind string) bool {
	switch kind {
	case GroupEventOnline, GroupEventOffline, GroupEventNameChange, GroupEventDescriptionChange, GroupEventRateChange:
		return events.AllowsGroupEvent(kind)
	case "group_changes":
		return events.HasGroupEvents()
	case "group_change", "catalog_change":
		return events.UpstreamGroupChange
	case "rate_change":
		return events.UpstreamRateChange
	case "pricing_change":
		// Pricing automation retains its site-level notification controls.
		return true
	case "protection_change":
		return true
	case "local_group_change":
		// Local group changes are delivered through AstrBot/QQ. Keep them out
		// of the email notification fan-out even when an old persisted config
		// still has the legacy flag enabled.
		return false
	case "health_change", "balance_change", "key_health_change":
		return events.HealthChange
	default:
		// Other monitoring policies have their own explicit notification gates.
		return false
	}
}

type GlobalLocalMonitorSettings struct {
	Enabled         bool  `json:"enabled"`
	IntervalSeconds int64 `json:"interval_seconds"`
}

type GlobalCadenceDefaults struct {
	FastIntervalSeconds int64 `json:"fast_interval_seconds"`
	FullIntervalSeconds int64 `json:"full_interval_seconds"`
}

type AstrBotTarget struct {
	UMO     string `json:"umo"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

type AstrBotSettings struct {
	Enabled            bool                     `json:"enabled"`
	BaseURL            string                   `json:"base_url"`
	EndpointPath       string                   `json:"endpoint_path"`
	APIKey             string                   `json:"api_key"`
	APIKeyConfigured   bool                     `json:"api_key_configured"`
	ClearAPIKey        bool                     `json:"clear_api_key,omitempty"`
	Events             GlobalNotificationEvents `json:"events"`
	Targets            []AstrBotTarget          `json:"targets"`
	TimeoutSeconds     int64                    `json:"timeout_seconds"`
	apiKeyCiphertext   string
	ciphertextProvided bool
}

// GlobalSettings is persisted as one versioned JSON value in the existing
// settings table. The AstrBot credential is encrypted separately from the
// public settings fields and is resolved only for runtime delivery.
type GlobalSettings struct {
	Version       int64                      `json:"version"`
	Notifications GlobalNotificationSettings `json:"notifications"`
	LocalMonitor  GlobalLocalMonitorSettings `json:"local_monitor"`
	Defaults      GlobalCadenceDefaults      `json:"defaults"`
	AstrBot       AstrBotSettings            `json:"astrbot"`
}

// UnmarshalJSON migrates legacy flags for both stored settings and API writes.
// Presence checks keep an explicitly disabled granular event disabled.
func (value *GlobalSettings) UnmarshalJSON(data []byte) error {
	type wireSettings GlobalSettings
	next := wireSettings(defaultGlobalSettings())
	next.Version = 0
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	var fields struct {
		Notifications struct {
			Events map[string]json.RawMessage `json:"events"`
		} `json:"notifications"`
		AstrBot struct {
			Events           map[string]json.RawMessage `json:"events"`
			APIKeyCiphertext json.RawMessage            `json:"api_key_ciphertext"`
		} `json:"astrbot"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	migrate := func(events *GlobalNotificationEvents, present map[string]json.RawMessage, group, rate bool) {
		for key, target := range map[string]*bool{
			GroupEventOnline: &events.GroupOnline, GroupEventOffline: &events.GroupOffline,
			GroupEventNameChange: &events.GroupNameChange, GroupEventDescriptionChange: &events.GroupDescriptionChange,
		} {
			if _, ok := present[key]; !ok {
				*target = group
			}
		}
		if _, ok := present[GroupEventRateChange]; !ok {
			events.GroupRateChange = rate
		}
	}
	migrate(&next.Notifications.Events, fields.Notifications.Events, next.Notifications.Events.UpstreamGroupChange, next.Notifications.Events.UpstreamRateChange)
	migrate(&next.AstrBot.Events, fields.AstrBot.Events, next.AstrBot.Events.LocalGroupChange, next.AstrBot.Events.LocalGroupChange)
	next.AstrBot.ciphertextProvided = len(fields.AstrBot.APIKeyCiphertext) > 0
	*value = GlobalSettings(next)
	return nil
}

func defaultGlobalSettings() GlobalSettings {
	return GlobalSettings{
		Version: 1,
		Notifications: GlobalNotificationSettings{
			Enabled:    false,
			Recipients: []string{},
			Events: GlobalNotificationEvents{
				GroupOnline: true, GroupOffline: true, GroupNameChange: true, GroupDescriptionChange: true, GroupRateChange: true,
				UpstreamGroupChange: true,
				UpstreamRateChange:  true,
				PricingChange:       true,
				ProtectionChange:    true,
				LocalGroupChange:    false,
				HealthChange:        true,
			},
		},
		LocalMonitor: GlobalLocalMonitorSettings{Enabled: false, IntervalSeconds: 60},
		Defaults:     GlobalCadenceDefaults{FastIntervalSeconds: 60, FullIntervalSeconds: 900},
		AstrBot: AstrBotSettings{
			Enabled: false, EndpointPath: "/api/v1/im/message", Targets: []AstrBotTarget{}, TimeoutSeconds: 10,
			Events: GlobalNotificationEvents{LocalGroupChange: true, GroupOnline: true, GroupOffline: true, GroupNameChange: true, GroupDescriptionChange: true, GroupRateChange: true},
		},
	}
}

func normalizeGlobalSettings(value GlobalSettings) (GlobalSettings, error) {
	defaults := defaultGlobalSettings()
	if value.Version <= 0 {
		value.Version = defaults.Version
	}
	if value.Notifications.Recipients == nil {
		value.Notifications.Recipients = []string{}
	}
	if value.LocalMonitor.IntervalSeconds == 0 {
		value.LocalMonitor.IntervalSeconds = defaults.LocalMonitor.IntervalSeconds
	}
	if value.Defaults.FastIntervalSeconds == 0 {
		value.Defaults.FastIntervalSeconds = defaults.Defaults.FastIntervalSeconds
	}
	if value.Defaults.FullIntervalSeconds == 0 {
		value.Defaults.FullIntervalSeconds = defaults.Defaults.FullIntervalSeconds
	}
	if value.AstrBot.EndpointPath == "" {
		value.AstrBot.EndpointPath = defaults.AstrBot.EndpointPath
	}
	if value.AstrBot.Targets == nil {
		value.AstrBot.Targets = []AstrBotTarget{}
	}
	if value.AstrBot.TimeoutSeconds == 0 {
		value.AstrBot.TimeoutSeconds = defaults.AstrBot.TimeoutSeconds
	}
	// The legacy local_group_change flag lived under email notifications. It
	// is now owned by AstrBot so existing settings cannot accidentally route
	// local group changes to email recipients.
	value.Notifications.Events.LocalGroupChange = false
	if len(value.Notifications.Recipients) > 100 {
		return GlobalSettings{}, ErrInvalid
	}
	for i, recipient := range value.Notifications.Recipients {
		value.Notifications.Recipients[i] = strings.TrimSpace(recipient)
		if value.Notifications.Recipients[i] == "" {
			return GlobalSettings{}, ErrInvalid
		}
	}
	if err := validateGlobalURL(value.AstrBot.BaseURL); err != nil {
		return GlobalSettings{}, err
	}
	if value.AstrBot.EndpointPath != "" && !strings.HasPrefix(value.AstrBot.EndpointPath, "/") {
		return GlobalSettings{}, ErrInvalid
	}
	if value.LocalMonitor.IntervalSeconds < 1 || value.LocalMonitor.IntervalSeconds > maxGlobalCadenceSeconds {
		return GlobalSettings{}, ErrInvalid
	}
	if value.Defaults.FastIntervalSeconds < 1 || value.Defaults.FastIntervalSeconds > maxGlobalCadenceSeconds || value.Defaults.FullIntervalSeconds < 1 || value.Defaults.FullIntervalSeconds > maxGlobalCadenceSeconds {
		return GlobalSettings{}, ErrInvalid
	}
	if value.AstrBot.TimeoutSeconds < 1 || value.AstrBot.TimeoutSeconds > 600 {
		return GlobalSettings{}, ErrInvalid
	}
	if len(value.AstrBot.Targets) > 100 {
		return GlobalSettings{}, ErrInvalid
	}
	for i := range value.AstrBot.Targets {
		value.AstrBot.Targets[i].UMO = strings.TrimSpace(value.AstrBot.Targets[i].UMO)
		value.AstrBot.Targets[i].Label = strings.TrimSpace(value.AstrBot.Targets[i].Label)
		if value.AstrBot.Targets[i].UMO == "" {
			return GlobalSettings{}, ErrInvalid
		}
	}
	return value, nil
}

func validateGlobalURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return ErrInvalid
	}
	return nil
}

// ParseGlobalSettings decodes the persisted setting and applies compatibility
// defaults. It is exported for notification adapters that share this setting
// store without taking a dependency on persistence details.
func ParseGlobalSettings(raw string) (GlobalSettings, error) {
	value := defaultGlobalSettings()
	if strings.TrimSpace(raw) == "" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return GlobalSettings{}, err
	}
	if value.Version <= 0 {
		value.Version = 1
	}
	if err := parseGlobalSettingsSecrets(raw, &value); err != nil {
		return GlobalSettings{}, err
	}
	return normalizeGlobalSettings(value)
}

func (s *Service) GetGlobalSettings(ctx context.Context) (GlobalSettings, error) {
	defaults := defaultGlobalSettings()
	if s == nil || s.globalSettingsStore == nil {
		return defaults, nil
	}
	values, err := s.globalSettingsStore.GetMultiple(ctx, []string{globalSettingsKey})
	if err != nil {
		return GlobalSettings{}, err
	}
	value, err := ParseGlobalSettings(values[globalSettingsKey])
	if err != nil {
		return GlobalSettings{}, err
	}
	return value, nil
}

func (s *Service) SaveGlobalSettings(ctx context.Context, input GlobalSettings) (GlobalSettings, error) {
	if s == nil || s.globalSettingsStore == nil {
		return GlobalSettings{}, ErrUnsupported
	}
	// Keep the version check and write together within this service instance,
	// including preservation of an existing credential from the current value.
	s.globalSettingsMu.Lock()
	defer s.globalSettingsMu.Unlock()
	current, err := s.GetGlobalSettings(ctx)
	if err != nil {
		return GlobalSettings{}, err
	}
	if input.Version != current.Version {
		return GlobalSettings{}, ErrConflict
	}
	if input.AstrBot.ciphertextProvided {
		return GlobalSettings{}, ErrInvalid
	}
	input.Version = current.Version + 1
	input, err = normalizeGlobalSettings(input)
	if err != nil {
		return GlobalSettings{}, err
	}
	input, err = s.prepareGlobalSettingsSecrets(input, current)
	if err != nil {
		return GlobalSettings{}, err
	}
	raw, err := marshalGlobalSettings(input)
	if err != nil {
		return GlobalSettings{}, err
	}
	if err := s.globalSettingsStore.SetMultiple(ctx, map[string]string{globalSettingsKey: string(raw)}); err != nil {
		return GlobalSettings{}, err
	}
	return input, nil
}
