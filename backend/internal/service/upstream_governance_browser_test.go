package service

import (
	"context"
	"testing"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

type browserProxyRepository struct {
	ProxyRepository
	value *Proxy
}

func (p *browserProxyRepository) GetByID(context.Context, int64) (*Proxy, error) { return p.value, nil }

func TestGovernanceBrowserNativeVerificationPinsProxy(t *testing.T) {
	repo := &browserProxyRepository{value: &Proxy{ID: 1, Protocol: "http", Host: "proxy-a.example", Port: 8080, Status: StatusActive}}
	id := int64(1)
	site := gov.Site{ProxyID: &id}
	factory := governanceClientFactory(nil, repo)
	ctx := gov.WithBrowserAuthorizationProxy(t.Context(), repo.value.URL())
	_, err := factory(ctx, site)
	require.NoError(t, err)
	repo.value.Host = "proxy-b.example"
	_, err = factory(ctx, site)
	require.ErrorIs(t, err, gov.ErrConflict)
	_, err = factory(gov.WithBrowserAuthorizationProxy(t.Context(), ""), site)
	require.ErrorIs(t, err, gov.ErrConflict)
	_, err = factory(gov.WithBrowserAuthorizationProxy(t.Context(), ""), gov.Site{})
	require.NoError(t, err)
}
