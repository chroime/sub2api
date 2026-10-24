package upstreamgovernance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func globalCadenceEngine(t *testing.T) (*Service, *memoryStore) {
	t.Helper()
	store := &memoryStore{site: Site{
		ID: 1, Version: 1, Name: "Existing upstream", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true,
		IntervalMinutes: 3, FastIntervalSeconds: 5, FullIntervalSeconds: 125,
	}}
	settings := defaultGlobalSettings()
	settings.Defaults = GlobalCadenceDefaults{FastIntervalSeconds: 17, FullIntervalSeconds: 420}
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	svc := NewService(store, nil, nil, nil, false)
	svc.SetGlobalSettingsStore(&globalSettingsMemoryStore{values: map[string]string{GlobalSettingsKey: string(raw)}})
	return svc, store
}

func TestGlobalCadenceDefaultsCreateOnlyFillsUnspecifiedIntervals(t *testing.T) {
	for _, tt := range []struct {
		name               string
		minutes            int
		fast, full         int64
		wantMinutes        int
		wantFast, wantFull int64
	}{
		{name: "inherit both defaults", wantMinutes: 7, wantFast: 17, wantFull: 420},
		{name: "legacy minutes remain explicit", minutes: 2, wantMinutes: 2, wantFast: 120, wantFull: 120},
		{name: "explicit fast inherits full", fast: 9, wantMinutes: 7, wantFast: 9, wantFull: 420},
		{name: "explicit full inherits fast", full: 125, wantMinutes: 3, wantFast: 17, wantFull: 125},
		{name: "explicit seconds override defaults", fast: 9, full: 125, wantMinutes: 3, wantFast: 9, wantFull: 125},
		{name: "explicit fast with legacy minutes", minutes: 2, fast: 9, wantMinutes: 2, wantFast: 9, wantFull: 120},
		{name: "explicit full overrides minute alias", minutes: 2, full: 125, wantMinutes: 3, wantFast: 17, wantFull: 125},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := globalCadenceEngine(t)
			created, err := svc.CreateSite(t.Context(), Site{
				Name: "New upstream", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true,
				IntervalMinutes: tt.minutes, FastIntervalSeconds: tt.fast, FullIntervalSeconds: tt.full,
			})
			require.NoError(t, err)
			require.Equal(t, "New upstream", created.Name)
			require.Equal(t, tt.wantMinutes, store.site.IntervalMinutes)
			require.Equal(t, tt.wantFast, store.site.FastIntervalSeconds)
			require.Equal(t, tt.wantFull, store.site.FullIntervalSeconds)
		})
	}
}

func TestGlobalCadenceDefaultsUpdatePreservesUnspecifiedIntervals(t *testing.T) {
	for _, tt := range []struct {
		name               string
		minutes            int
		fast, full         int64
		wantMinutes        int
		wantFast, wantFull int64
	}{
		{name: "name only with existing minute alias", minutes: 3, wantMinutes: 3, wantFast: 5, wantFull: 125},
		{name: "all cadence fields omitted", wantMinutes: 3, wantFast: 5, wantFull: 125},
		{name: "fast only", minutes: 3, fast: 9, wantMinutes: 3, wantFast: 9, wantFull: 125},
		{name: "full only", minutes: 3, full: 240, wantMinutes: 4, wantFast: 5, wantFull: 240},
		{name: "legacy minutes update both intervals", minutes: 4, wantMinutes: 4, wantFast: 240, wantFull: 240},
		{name: "legacy minutes with explicit fast", minutes: 4, fast: 9, wantMinutes: 4, wantFast: 9, wantFull: 240},
		{name: "explicit full takes precedence over minute alias", minutes: 4, full: 300, wantMinutes: 5, wantFast: 5, wantFull: 300},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := globalCadenceEngine(t)
			updated, err := svc.UpdateSite(t.Context(), 1, Site{
				Version: 1, Name: "Renamed upstream", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true,
				IntervalMinutes: tt.minutes, FastIntervalSeconds: tt.fast, FullIntervalSeconds: tt.full,
			})
			require.NoError(t, err)
			require.Equal(t, "Renamed upstream", updated.Name)
			require.EqualValues(t, 2, updated.Version)
			require.Equal(t, tt.wantMinutes, store.site.IntervalMinutes)
			require.Equal(t, tt.wantFast, store.site.FastIntervalSeconds)
			require.Equal(t, tt.wantFull, store.site.FullIntervalSeconds)
		})
	}
}

func TestGlobalCadenceDefaultsUpdatePreservesLegacyStoredIntervals(t *testing.T) {
	svc, store := globalCadenceEngine(t)
	store.site.IntervalMinutes = 2
	store.site.FastIntervalSeconds = 0
	store.site.FullIntervalSeconds = 0
	_, err := svc.UpdateSite(t.Context(), 1, Site{
		Version: 1, Name: "Renamed upstream", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true,
		FullIntervalSeconds: 240,
	})
	require.NoError(t, err)
	require.EqualValues(t, 120, store.site.FastIntervalSeconds)
	require.EqualValues(t, 240, store.site.FullIntervalSeconds)
}

func TestGlobalCadenceDefaultsDoNotMaskInvalidIntervals(t *testing.T) {
	for _, tt := range []struct {
		name       string
		minutes    int
		fast, full int64
	}{
		{name: "negative fast", fast: -1},
		{name: "negative full", full: -1},
		{name: "negative minutes", minutes: -1},
		{name: "unrepresentable minutes", minutes: maxIntervalMinutes + 1},
		{name: "negative minutes with explicit full", minutes: -1, full: 125},
		{name: "unrepresentable minutes with explicit full", minutes: maxIntervalMinutes + 1, full: 125},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := Site{
				Version: 1, Name: "Invalid upstream", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true,
				IntervalMinutes: tt.minutes, FastIntervalSeconds: tt.fast, FullIntervalSeconds: tt.full,
			}
			for _, operation := range []string{"create", "update"} {
				t.Run(operation, func(t *testing.T) {
					svc, store := globalCadenceEngine(t)
					var err error
					if operation == "create" {
						_, err = svc.CreateSite(t.Context(), input)
					} else {
						_, err = svc.UpdateSite(t.Context(), 1, input)
					}
					require.ErrorIs(t, err, ErrInvalid)
					require.EqualValues(t, 1, store.site.Version)
					require.Equal(t, "Existing upstream", store.site.Name)
				})
			}
		})
	}
}
