package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type operationsPagedSitesStore struct {
	Store
	first     []Site
	cursors   []int64
	pageError error
}

func (m *operationsPagedSitesStore) ListSites(context.Context) ([]Site, error) {
	return m.first, nil
}

func (m *operationsPagedSitesStore) ListSitesAfter(_ context.Context, after int64, limit int) ([]Site, error) {
	m.cursors = append(m.cursors, after)
	if after == 0 {
		return m.first, nil
	}
	if m.pageError != nil {
		return nil, m.pageError
	}
	return []Site{{ID: 1001, Name: "Later upstream"}}, nil
}

func TestOperationsSiteNavigationInventoryIncludesSitesAfterFirstThousand(t *testing.T) {
	m := &operationsPagedSitesStore{first: make([]Site, 1000)}
	for i := range m.first {
		m.first[i] = Site{ID: int64(i + 1)}
	}
	svc := NewService(m, nil, nil, nil, false)
	sites, err := svc.ListSites(t.Context())
	require.NoError(t, err)
	require.Len(t, sites, 1001, "every workbench destination must also be reachable from the site inventory")
	require.Equal(t, int64(1001), sites[1000].ID)
	require.Equal(t, []int64{0, 1000}, m.cursors)
}

type balanceSitesStore struct {
	Store
	sites    []Site
	balances map[int64]SiteBalance
	err      error
}

func (m *balanceSitesStore) ListSites(context.Context) ([]Site, error) { return m.sites, nil }
func (m *balanceSitesStore) LatestSiteBalances(context.Context) (map[int64]SiteBalance, error) {
	return m.balances, m.err
}

func TestSiteInventoryCarriesTheLatestCollectedBalance(t *testing.T) {
	observed := time.Date(2026, 11, 5, 15, 56, 0, 0, time.UTC)
	m := &balanceSitesStore{
		sites:    []Site{{ID: 1, Name: "Collected"}, {ID: 2, Name: "Never collected"}},
		balances: map[int64]SiteBalance{1: {Amount: 8.15, Unit: "usd", ObservedAt: observed}},
	}
	sites, err := NewService(m, nil, nil, nil, false).ListSites(t.Context())
	require.NoError(t, err)
	require.Equal(t, &SiteBalance{Amount: 8.15, Unit: "usd", ObservedAt: observed}, sites[0].Balance)
	require.Nil(t, sites[1].Balance)
}

func TestSiteInventoryStillListsSitesWhenBalancesCannotBeRead(t *testing.T) {
	m := &balanceSitesStore{sites: []Site{{ID: 1, Name: "Collected"}}, err: errors.New("read failure")}
	sites, err := NewService(m, nil, nil, nil, false).ListSites(t.Context())
	require.NoError(t, err)
	require.Len(t, sites, 1)
	require.Nil(t, sites[0].Balance)
}

func TestOperationsSiteNavigationInventoryDoesNotReturnTruncatedSuccessOnPageFailure(t *testing.T) {
	m := &operationsPagedSitesStore{first: make([]Site, 1000), pageError: errors.New("read failure")}
	for i := range m.first {
		m.first[i] = Site{ID: int64(i + 1)}
	}
	sites, err := NewService(m, nil, nil, nil, false).ListSites(t.Context())
	require.ErrorIs(t, err, m.pageError)
	require.Nil(t, sites)
}
