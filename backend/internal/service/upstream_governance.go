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
	"github.com/lib/pq"
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
	return gov.LocalTarget{ID: g.ID, Name: g.Name, Platform: g.Platform, SaleMultiplier: g.RateMultiplier, Fingerprint: GovernanceGroupFingerprint(g)}, nil
}

// GovernanceGroupFingerprint is also checked while every target row is locked
// by persistence, so a concurrent group edit cannot invalidate an import.
func GovernanceGroupFingerprint(g *Group) string {
	// Exclude live account counters and hydration metadata from preview staleness.
	state := *g
	state.AccountGroups = nil
	state.AccountCount, state.ActiveAccountCount, state.RateLimitedAccountCount = 0, 0, 0
	state.Hydrated = false
	return governanceFingerprint(state)
}
func governanceLocal(a *Account) *gov.LocalAccount {
	ids := append([]int64{}, a.GroupIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	config := governanceAccountConfig(a)
	extra := make(map[string]any, len(a.Extra))
	for key, value := range a.Extra {
		extra[key] = value
	}
	// Runtime accounting and probe refreshes do not invalidate reviewed settings.
	for _, key := range []string{"quota_used", "quota_daily_used", "quota_weekly_used", "quota_daily_start", "quota_weekly_start", "quota_daily_reset_at", "quota_weekly_reset_at", UpstreamBillingProbeExtraKey} {
		delete(extra, key)
	}
	rate := a.BillingRateMultiplier()
	if config.UpstreamBillingRateSyncEnabled {
		rate = 0 // The upstream owns this value while synchronization is enabled.
	}
	key, _ := a.Credentials["api_key"].(string)
	return &gov.LocalAccount{ID: a.ID, Name: a.Name, GroupIDs: ids, CostMultiplier: a.BillingRateMultiplier(), AccountConfig: config, NotesMatchAPIKey: key != "" && a.Notes != nil && *a.Notes == key, BillingProbeEnabled: upstreamBillingProbeEnabled(a), Fingerprint: governanceFingerprint(struct {
		ID                           int64
		Name, Platform, Type, Status string
		Notes                        *string
		Concurrency, Priority        int
		Credentials, Extra           map[string]any
		ProxyID, ParentAccountID     *int64
		Groups                       []int64
		Rate                         float64
	}{a.ID, a.Name, a.Platform, a.Type, a.Status, a.Notes, a.Concurrency, a.Priority, a.Credentials, extra, a.ProxyID, a.ParentAccountID, ids, rate})}
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

func (l *governanceLocalAccounts) AccountNames(ctx context.Context, ids []int64) (map[int64]gov.LocalAccountName, error) {
	names := make(map[int64]gov.LocalAccountName, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := l.db.QueryContext(ctx, `SELECT id, name, deleted_at IS NOT NULL FROM accounts WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var label gov.LocalAccountName
		if err := rows.Scan(&id, &label.Name, &label.Deleted); err != nil {
			return nil, err
		}
		names[id] = label
	}
	return names, rows.Err()
}
func governanceDesired(a *Account, c gov.AccountChange) bool {
	config := c.AccountConfig
	ids, err := gov.NormalizeLocalGroupIDs(a.GroupIDs, 0)
	return config != nil && a.ParentAccountID == nil && a.Extra[governanceMarkerKey] == c.Marker && a.Type == "apikey" && a.Platform == c.Platform && a.Name == c.Name &&
		a.Notes != nil && *a.Notes == c.APIKey && a.Credentials["api_key"] == c.APIKey && a.Credentials["base_url"] == c.BaseURL &&
		reflect.DeepEqual(a.ProxyID, c.ProxyID) && err == nil && reflect.DeepEqual(ids, c.GroupIDs) &&
		(config.UpstreamBillingRateSyncEnabled || a.BillingRateMultiplier() == c.CostMultiplier) &&
		upstreamBillingProbeEnabled(a) == config.UpstreamBillingRateSyncEnabled && reflect.DeepEqual(governanceAccountConfig(a), config)
}
func (l *governanceLocalAccounts) ApplyAccount(ctx context.Context, c gov.AccountChange) (*gov.LocalAccount, error) {
	if c.Marker == "" || c.APIKey == "" {
		return nil, gov.ErrInvalid
	}
	var e error
	c.AccountConfig, e = gov.NormalizeAccountConfig(c.AccountConfig, c.Platform, nil)
	if e != nil {
		return nil, e
	}
	c.GroupIDs, e = gov.NormalizeLocalGroupIDs(c.GroupIDs, c.GroupID)
	if e != nil {
		return nil, e
	}
	c.GroupID = c.GroupIDs[0]
	targets := make(map[int64]string, len(c.GroupIDs))
	for _, id := range c.GroupIDs {
		target, err := l.Target(ctx, id, c.Platform)
		if err != nil {
			return nil, err
		}
		expected := c.ExpectedTargetFingerprints[id]
		if expected == "" && id == c.GroupID {
			expected = c.ExpectedTargetFingerprint
		}
		if expected != "" && target.Fingerprint != expected {
			return nil, gov.ErrConflict
		}
		targets[id] = target.Fingerprint
	}
	a, e := l.find(ctx, c.Marker)
	if e != nil {
		return nil, e
	}
	config := c.AccountConfig
	syncEnabled := config.UpstreamBillingRateSyncEnabled
	if a != nil {
		if governanceDesired(a, c) {
			return governanceLocal(a), nil
		}
		if a.ParentAccountID != nil || c.ExpectedFingerprint == "" || governanceLocal(a).Fingerprint != c.ExpectedFingerprint || a.Platform != c.Platform || a.Type != "apikey" {
			return nil, gov.ErrConflict
		}
		proxy := c.ProxyID
		if proxy == nil {
			zero := int64(0)
			proxy = &zero
		}
		groups := c.GroupIDs
		var rate *float64
		if !syncEnabled {
			rate = &c.CostMultiplier
		}
		a, e = l.admin.UpdateAccount(withGovernanceMutation(ctx, c.ExpectedFingerprint, groups, targets), a.ID, &UpdateAccountInput{Name: c.Name, Notes: &c.APIKey, Credentials: governanceImportCredentials(a.Credentials, c), Extra: governanceImportExtra(a.Extra, c), ProxyID: proxy, Concurrency: &config.Concurrency, Priority: config.Priority, RateMultiplier: rate, GroupIDs: &groups, ProbeEnabled: &syncEnabled, RateSyncEnabled: &syncEnabled})
	} else {
		if c.ExpectedFingerprint != "" {
			return nil, gov.ErrConflict
		}
		a, e = l.admin.CreateAccount(withGovernanceMutation(ctx, "", c.GroupIDs, targets), &CreateAccountInput{Name: c.Name, Notes: &c.APIKey, Platform: c.Platform, Type: "apikey", Credentials: governanceImportCredentials(nil, c), Extra: governanceImportExtra(nil, c), ProxyID: c.ProxyID, Concurrency: config.Concurrency, Priority: *config.Priority, RateMultiplier: &c.CostMultiplier, GroupIDs: c.GroupIDs, ProbeEnabled: &syncEnabled, RateSyncEnabled: &syncEnabled})
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
func ProvideUpstreamGovernanceService(db *sql.DB, admin AdminService, upstream HTTPUpstream, proxies ProxyRepository, cipher SecretEncryptor, cfg *config.Config, email *EmailService, settings SettingRepository, users UserRepository) *gov.Service {
	svc := gov.NewService(gov.NewSQLStore(db), gov.NewConnector(governanceClientFactory(upstream, proxies)), &governanceLocalAccounts{db: db, admin: admin}, cipher, cfg != nil && cfg.Totp.EncryptionKeyConfigured)
	notifier := &governanceBalanceNotifier{settings: settings, admins: users}
	if email != nil {
		notifier.mail = email
	}
	svc.SetBalanceNotifier(notifier)
	svc.Start()
	return svc
}
