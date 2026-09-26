package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

const governanceMarkerKey = "upstream_governance_marker"

type governanceAdmin interface {
	GetGroup(context.Context, int64) (*Group, error)
	GetAccount(context.Context, int64) (*Account, error)
	CreateAccount(context.Context, *CreateAccountInput) (*Account, error)
	UpdateAccount(context.Context, int64, *UpdateAccountInput) (*Account, error)
}
type governanceLocalAccounts struct {
	db    *sql.DB
	admin governanceAdmin
}

func governanceFingerprint(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (l *governanceLocalAccounts) Target(ctx context.Context, id int64, platform string) (gov.LocalTarget, error) {
	g, e := l.admin.GetGroup(ctx, id)
	if e != nil {
		return gov.LocalTarget{}, e
	}
	if g == nil || g.Status != StatusActive || (g.Platform != platform && g.Platform != "composite") {
		return gov.LocalTarget{}, gov.ErrInvalid
	}
	// Exclude live account counters and hydration metadata from preview staleness.
	state := *g
	state.AccountGroups = nil
	state.AccountCount, state.ActiveAccountCount, state.RateLimitedAccountCount = 0, 0, 0
	state.Hydrated = false
	return gov.LocalTarget{ID: g.ID, Name: g.Name, Platform: g.Platform, SaleMultiplier: g.RateMultiplier, Fingerprint: governanceFingerprint(state)}, nil
}
func governanceLocal(a *Account) *gov.LocalAccount {
	ids := append([]int64{}, a.GroupIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return &gov.LocalAccount{ID: a.ID, Name: a.Name, GroupIDs: ids, CostMultiplier: a.BillingRateMultiplier(), Fingerprint: governanceFingerprint(struct {
		ID                           int64
		Name, Platform, Type, Status string
		Credentials, Extra           map[string]any
		ProxyID, ParentAccountID     *int64
		Groups                       []int64
		Rate                         float64
	}{a.ID, a.Name, a.Platform, a.Type, a.Status, a.Credentials, a.Extra, a.ProxyID, a.ParentAccountID, ids, a.BillingRateMultiplier()})}
}
func (l *governanceLocalAccounts) find(ctx context.Context, marker string) (*Account, error) {
	var id int64
	e := l.db.QueryRowContext(ctx, `SELECT id FROM accounts WHERE deleted_at IS NULL AND extra->>'upstream_governance_marker' = $1`, marker).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	a, e := l.admin.GetAccount(ctx, id)
	if e != nil {
		return nil, e
	}
	if a == nil || a.Extra[governanceMarkerKey] != marker {
		return nil, gov.ErrConflict
	}
	return a, nil
}
func (l *governanceLocalAccounts) FindAccount(ctx context.Context, marker string) (*gov.LocalAccount, error) {
	a, e := l.find(ctx, marker)
	if e != nil || a == nil {
		return nil, e
	}
	return governanceLocal(a), nil
}
func governanceDesired(a *Account, c gov.AccountChange) bool {
	probe, _ := a.Extra[UpstreamBillingProbeEnabledExtraKey].(bool)
	sync, _ := a.Extra[UpstreamBillingRateSyncEnabledExtraKey].(bool)
	return a.ParentAccountID == nil && a.Extra[governanceMarkerKey] == c.Marker && a.Type == "apikey" && a.Platform == c.Platform && a.Name == c.Name && a.Credentials["api_key"] == c.APIKey && a.Credentials["base_url"] == c.BaseURL && reflect.DeepEqual(a.ProxyID, c.ProxyID) && len(a.GroupIDs) == 1 && a.GroupIDs[0] == c.GroupID && a.BillingRateMultiplier() == c.CostMultiplier && !probe && !sync
}
func (l *governanceLocalAccounts) ApplyAccount(ctx context.Context, c gov.AccountChange) (*gov.LocalAccount, error) {
	if c.Marker == "" || c.APIKey == "" {
		return nil, gov.ErrInvalid
	}
	target, e := l.Target(ctx, c.GroupID, c.Platform)
	if e != nil {
		return nil, e
	}
	if c.ExpectedTargetFingerprint != "" && target.Fingerprint != c.ExpectedTargetFingerprint {
		return nil, gov.ErrConflict
	}
	a, e := l.find(ctx, c.Marker)
	if e != nil {
		return nil, e
	}
	disabled := false
	if a != nil {
		if governanceDesired(a, c) {
			return governanceLocal(a), nil
		}
		if a.ParentAccountID != nil || c.ExpectedFingerprint == "" || governanceLocal(a).Fingerprint != c.ExpectedFingerprint || a.Platform != c.Platform || a.Type != "apikey" {
			return nil, gov.ErrConflict
		}
		credentials := map[string]any{}
		for k, v := range a.Credentials {
			credentials[k] = v
		}
		credentials["api_key"] = c.APIKey
		credentials["base_url"] = c.BaseURL
		proxy := c.ProxyID
		if proxy == nil {
			zero := int64(0)
			proxy = &zero
		}
		groups := []int64{c.GroupID}
		a, e = l.admin.UpdateAccount(withGovernanceMutation(ctx, c.ExpectedFingerprint, groups), a.ID, &UpdateAccountInput{Name: c.Name, Credentials: credentials, ProxyID: proxy, RateMultiplier: &c.CostMultiplier, GroupIDs: &groups, ProbeEnabled: &disabled, RateSyncEnabled: &disabled})
	} else {
		if c.ExpectedFingerprint != "" {
			return nil, gov.ErrConflict
		}
		a, e = l.admin.CreateAccount(withGovernanceMutation(ctx, "", []int64{c.GroupID}), &CreateAccountInput{Name: c.Name, Platform: c.Platform, Type: "apikey", Credentials: map[string]any{"api_key": c.APIKey, "base_url": c.BaseURL}, Extra: map[string]any{governanceMarkerKey: c.Marker}, ProxyID: c.ProxyID, Concurrency: 1, RateMultiplier: &c.CostMultiplier, GroupIDs: []int64{c.GroupID}, ProbeEnabled: &disabled})
		if e != nil {
			recovered, re := l.find(ctx, c.Marker)
			if re == nil && recovered != nil && governanceDesired(recovered, c) {
				return governanceLocal(recovered), nil
			}
		}
	}
	if e != nil {
		return nil, e
	}
	return governanceLocal(a), nil
}

type governanceHTTPClient struct {
	upstream HTTPUpstream
	proxy    string
}

func (c governanceHTTPClient) Do(req *http.Request) (*http.Response, error) {
	ctx := WithHTTPUpstreamPublicHostsOnly(WithHTTPUpstreamRedirectsDisabled(req.Context()))
	return c.upstream.Do(req.WithContext(ctx), c.proxy, 0, 2)
}
func governanceClientFactory(upstream HTTPUpstream, proxies ProxyRepository) gov.ClientFactory {
	return func(ctx context.Context, site gov.Site) (gov.HTTPDoer, error) {
		proxyURL := ""
		if site.ProxyID != nil {
			if *site.ProxyID <= 0 || proxies == nil {
				return nil, gov.ErrInvalid
			}
			p, e := proxies.GetByID(ctx, *site.ProxyID)
			if e != nil {
				return nil, gov.ErrInvalid
			}
			if p == nil || !p.IsActive() || p.IsExpired(time.Now()) || strings.TrimSpace(p.Host) == "" || p.Port < 1 || p.Port > 65535 {
				return nil, gov.ErrInvalid
			}
			switch p.Protocol {
			case "http", "https", "socks5", "socks5h":
			default:
				return nil, gov.ErrInvalid
			}
			proxyURL = p.URL()
		}
		return governanceHTTPClient{upstream: upstream, proxy: proxyURL}, nil
	}
}
func ProvideUpstreamGovernanceService(db *sql.DB, admin AdminService, upstream HTTPUpstream, proxies ProxyRepository, cipher SecretEncryptor, cfg *config.Config) *gov.Service {
	svc := gov.NewService(gov.NewSQLStore(db), gov.NewConnector(governanceClientFactory(upstream, proxies)), &governanceLocalAccounts{db: db, admin: admin}, cipher, cfg != nil && cfg.Totp.EncryptionKeyConfigured)
	svc.Start()
	return svc
}
