package upstreamgovernance

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type quotaUSDReaderForTest interface {
	QuotaPerUSD(context.Context, Site) *float64
}

func quotaReader(t *testing.T, c Connector) quotaUSDReaderForTest {
	t.Helper()
	r, ok := c.(quotaUSDReaderForTest)
	require.True(t, ok, "New API balances need optional public USD conversion metadata")
	return r
}

func displayMetadata(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	return fields
}

func TestQuotaDisplayReadsAndCachesPublicUSDScale(t *testing.T) {
	calls := 0
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		calls++
		require.Equal(t, "/api/status", r.URL.Path)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, defaultSessionUserAgent, r.UserAgent())
		for _, header := range []string{"Authorization", "Cookie", "New-Api-User"} {
			require.Empty(t, r.Header.Get(header), "public metadata must not carry a login session")
		}
		return 200, `{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"USD","display_in_currency":true,"price":7.3}}`
	})
	r := quotaReader(t, c)
	site := Site{ID: 18, Version: 2, Platform: "newapi", BaseURL: "https://upstream.example"}
	for range 2 {
		value := r.QuotaPerUSD(t.Context(), site)
		require.NotNil(t, value)
		require.Equal(t, 500000.0, *value, "price is not the quota divisor")
	}
	require.Equal(t, 1, calls, "opening multiple panels must reuse public metadata")
	proxy := int64(9)
	site.ProxyID = &proxy
	require.NotNil(t, r.QuotaPerUSD(t.Context(), site))
	require.Equal(t, 2, calls, "explicit proxy changes require a separate lookup")
}

func TestQuotaDisplayFallsBackWithoutAnExplicitValidUSDScale(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"missing currency", 200, `{"success":true,"data":{"quota_per_unit":500000,"display_in_currency":true}}`},
		{"other currency", 200, `{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"CNY"}}`},
		{"raw quota display", 200, `{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"USD","display_in_currency":false}}`},
		{"zero divisor", 200, `{"success":true,"data":{"quota_per_unit":0,"quota_display_type":"USD"}}`},
		{"negative divisor", 200, `{"success":true,"data":{"quota_per_unit":-1,"quota_display_type":"USD"}}`},
		{"overflow divisor", 200, `{"success":true,"data":{"quota_per_unit":1e309,"quota_display_type":"USD"}}`},
		{"unavailable", 404, `{}`},
		{"unauthorized public endpoint", 401, `{}`},
		{"rejected envelope", 200, `{"success":false,"data":{"quota_per_unit":500000,"quota_display_type":"USD"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := fixtureConnector(t, func(*http.Request) (int, string) {
				calls++
				return tc.code, tc.body
			})
			r := quotaReader(t, c)
			site := Site{Platform: "newapi", BaseURL: "https://upstream.example"}
			require.Nil(t, r.QuotaPerUSD(t.Context(), site))
			require.Nil(t, r.QuotaPerUSD(t.Context(), site))
			require.Equal(t, 1, calls, "unsupported metadata also needs a short cache")
			require.Nil(t, r.QuotaPerUSD(t.Context(), Site{Platform: "sub2api", BaseURL: site.BaseURL}))
			require.Equal(t, 1, calls, "USD-native sites do not need quota metadata")
		})
	}
}

func TestCatalogDisplaysLastKnownQuotaAsUSDWithoutChangingAccounting(t *testing.T) {
	balance, used := 4993033.0, 7004.0
	m := &memoryStore{
		site: Site{ID: 18, Version: 2, Platform: "newapi", BaseURL: "https://upstream.example", Status: "reauth_required"},
		snap: &Snapshot{ID: 524012, SiteID: 18, SiteVersion: 2, Catalog: Catalog{Account: &RemoteAccount{
			UserID: 262, Balance: &balance, UsedBalance: &used, Unit: "quota", Source: "/api/user/self",
		}}},
	}
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		require.Equal(t, "/api/status", r.URL.Path, "display correction must not retry an expired session")
		return 200, `{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"USD"}}`
	})
	snap, err := NewService(m, c, nil, nil, false).Catalog(t.Context(), 18)
	require.NoError(t, err)
	require.Equal(t, 500000.0, displayMetadata(t, snap.Catalog.Account)["quota_per_usd"])
	require.Equal(t, balance, *snap.Catalog.Account.Balance)
	require.Equal(t, used, *snap.Catalog.Account.UsedBalance)
	require.Equal(t, "quota", snap.Catalog.Account.Unit)
	require.Equal(t, m.snap.ID, snap.ID)
	require.NotSame(t, m.snap.Catalog.Account, snap.Catalog.Account)
	require.NotContains(t, displayMetadata(t, m.snap.Catalog.Account), "quota_per_usd", "read-time enrichment must not rewrite stored snapshots")
	require.Equal(t, "reauth_required", m.site.Status)
}

func TestSiteInventoryIncludesUSDScaleWithoutConvertingRawBalance(t *testing.T) {
	observed := time.Date(2026, 11, 12, 6, 7, 14, 0, time.UTC)
	m := &balanceSitesStore{
		sites:    []Site{{ID: 18, Platform: "newapi", BaseURL: "https://upstream.example"}},
		balances: map[int64]SiteBalance{18: {Amount: 4993033, Unit: "quota", ObservedAt: observed}},
	}
	c := fixtureConnector(t, func(*http.Request) (int, string) {
		return 200, `{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"USD"}}`
	})
	sites, err := NewService(m, c, nil, nil, false).ListSites(t.Context())
	require.NoError(t, err)
	require.Equal(t, 500000.0, displayMetadata(t, sites[0].Balance)["quota_per_usd"])
	require.Equal(t, 4993033.0, sites[0].Balance.Amount)
	require.Equal(t, "quota", sites[0].Balance.Unit)
	require.Equal(t, observed, sites[0].Balance.ObservedAt)
	require.NotContains(t, displayMetadata(t, m.balances[18]), "quota_per_usd")
}

func TestQuotaDisplayCoalescesConcurrentPanelReads(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	c := fixtureConnector(t, func(*http.Request) (int, string) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return 200, `{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"USD"}}`
	})
	r := quotaReader(t, c)
	site := Site{Platform: "newapi", BaseURL: "https://upstream.example"}
	var done sync.WaitGroup
	values := make(chan *float64, 10)
	for range 10 {
		done.Add(1)
		go func() { defer done.Done(); values <- r.QuotaPerUSD(t.Context(), site) }()
	}
	<-started
	close(release)
	done.Wait()
	close(values)
	for value := range values {
		require.NotNil(t, value)
		require.Equal(t, 500000.0, *value)
	}
	require.EqualValues(t, 1, calls.Load())
}

func TestSiteInventoryKeepsBalancesWhenPublicMetadataTimesOut(t *testing.T) {
	m := &balanceSitesStore{balances: make(map[int64]SiteBalance)}
	for i := int64(1); i <= 10; i++ {
		m.sites = append(m.sites, Site{ID: i, Version: i, Platform: "newapi", BaseURL: "https://upstream.example"})
		m.balances[i] = SiteBalance{Amount: 4993033, Unit: "quota"}
	}
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		<-r.Context().Done()
		return 503, `{}`
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	sites, err := NewService(m, c, nil, nil, false).ListSites(ctx)
	require.NoError(t, err, "optional display metadata must not turn a readable site list into an error")
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.Len(t, sites, 10)
	for _, site := range sites {
		require.Equal(t, 4993033.0, site.Balance.Amount)
		require.Nil(t, site.Balance.QuotaPerUSD)
	}
}

func TestCatalogKeepsRawBalanceWhenPublicMetadataIsUnavailable(t *testing.T) {
	balance := 4993033.0
	m := &memoryStore{
		site: Site{ID: 18, Version: 2, Platform: "newapi", BaseURL: "https://upstream.example", Status: "reauth_required"},
		snap: &Snapshot{SiteID: 18, SiteVersion: 2, Catalog: Catalog{Account: &RemoteAccount{Balance: &balance, Unit: "quota"}}},
	}
	c := fixtureConnector(t, func(*http.Request) (int, string) { return 401, `{}` })
	snap, err := NewService(m, c, nil, nil, false).Catalog(t.Context(), 18)
	require.NoError(t, err)
	require.Equal(t, balance, *snap.Catalog.Account.Balance)
	require.Nil(t, snap.Catalog.Account.QuotaPerUSD)
	require.Equal(t, "reauth_required", m.site.Status)
}
