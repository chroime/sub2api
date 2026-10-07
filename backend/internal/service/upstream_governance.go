package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
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

func governanceLocalModelTarget(row scanner) (gov.LocalModelTarget, error) {
	var target gov.LocalModelTarget
	var ownerID, groupID int64
	var key, keyName, keyStatus, groupName, groupPlatform, groupStatus string
	var expiresAt sql.NullTime
	var quota, quotaUsed float64
	var keyUpdatedAt, groupUpdatedAt time.Time
	if err := row.Scan(&target.APIKeyID, &ownerID, &key, &keyName, &groupID, &keyStatus, &expiresAt, &quota, &quotaUsed, &keyUpdatedAt, &target.GroupID, &groupName, &groupPlatform, &groupStatus, &groupUpdatedAt); err != nil {
		return gov.LocalModelTarget{}, err
	}
	if ownerID <= 0 || target.APIKeyID <= 0 || groupID <= 0 || target.GroupID != groupID || key == "" || keyStatus != StatusAPIKeyActive || groupStatus != StatusActive || (expiresAt.Valid && !expiresAt.Time.After(time.Now())) || (quota > 0 && quotaUsed >= quota) {
		return gov.LocalModelTarget{}, gov.ErrConflict
	}
	target.OwnerUserID, target.Key, target.APIKeyName, target.GroupName, target.Platform = ownerID, key, keyName, groupName, groupPlatform
	target.GroupFingerprint = governanceFingerprint(struct {
		ID                     int64
		Name, Platform, Status string
		UpdatedAt              time.Time
	}{target.GroupID, groupName, groupPlatform, groupStatus, groupUpdatedAt})
	target.KeyFingerprint = governanceFingerprint(struct {
		ID, OwnerID, GroupID int64
		Status               string
		ExpiresAt            *time.Time
		Quota                float64
		KeyHash              string
	}{target.APIKeyID, ownerID, groupID, keyStatus, nullTimePtr(expiresAt), quota, governanceFingerprint(key)})
	return target, nil
}

type scanner interface{ Scan(...any) error }

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}

const localModelTargetQuery = `SELECT k.id,k.user_id,k.key,k.name,k.group_id,k.status,k.expires_at,k.quota,k.quota_used,k.updated_at,g.id,g.name,g.platform,g.status,g.updated_at FROM api_keys k JOIN groups g ON g.id=k.group_id JOIN users u ON u.id=k.user_id WHERE k.deleted_at IS NULL AND g.deleted_at IS NULL AND u.deleted_at IS NULL AND u.status=$3 AND k.id=$1 AND k.user_id=$2`

func (l *governanceLocalAccounts) ResolveLocalModelTarget(ctx context.Context, ownerID, groupID, apiKeyID int64) (gov.LocalModelTarget, error) {
	if l == nil || l.db == nil || ownerID <= 0 || groupID <= 0 || apiKeyID <= 0 {
		return gov.LocalModelTarget{}, gov.ErrInvalid
	}
	target, err := governanceLocalModelTarget(l.db.QueryRowContext(ctx, localModelTargetQuery, apiKeyID, ownerID, StatusActive))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return gov.LocalModelTarget{}, gov.ErrNotFound
		}
		return gov.LocalModelTarget{}, err
	}
	if target.GroupID != groupID {
		return gov.LocalModelTarget{}, gov.ErrConflict
	}
	return target, nil
}

func (l *governanceLocalAccounts) ListLocalModelTargets(ctx context.Context, ownerID int64) ([]gov.LocalModelTarget, error) {
	if l == nil || l.db == nil || ownerID <= 0 {
		return nil, gov.ErrInvalid
	}
	rows, err := l.db.QueryContext(ctx, `SELECT k.id,k.user_id,k.key,k.name,k.group_id,k.status,k.expires_at,k.quota,k.quota_used,k.updated_at,g.id,g.name,g.platform,g.status,g.updated_at FROM api_keys k JOIN groups g ON g.id=k.group_id JOIN users u ON u.id=k.user_id WHERE k.deleted_at IS NULL AND g.deleted_at IS NULL AND u.deleted_at IS NULL AND u.status=$1 AND k.user_id=$2 AND k.status=$3 AND g.status=$4 AND (k.expires_at IS NULL OR k.expires_at>$5) AND (k.quota<=0 OR k.quota_used<k.quota) ORDER BY g.name,k.name,k.id`, StatusActive, ownerID, StatusAPIKeyActive, StatusActive, time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]gov.LocalModelTarget, 0)
	for rows.Next() {
		target, scanErr := governanceLocalModelTarget(rows)
		if scanErr != nil {
			if errors.Is(scanErr, gov.ErrConflict) {
				continue
			}
			return nil, scanErr
		}
		// Metadata responses never include the credential or fingerprints.
		target.Key = ""
		target.GroupFingerprint, target.KeyFingerprint, target.OwnerUserID = "", "", 0
		result = append(result, target)
	}
	return result, rows.Err()
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
	poolConcurrency := 2
	if gov.ModelRequestConcurrency(ctx) > 0 {
		// The model queue enforces its global and batch limits. Keep a stable,
		// separate streaming pool so an account-isolated pool of two connections
		// cannot silently serialize a larger requested test batch.
		poolConcurrency = 32
		ctx = WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileLongStream)
	}
	return c.upstream.Do(req.WithContext(ctx), c.proxy, 0, poolConcurrency)
}
func governanceClientFactory(upstream HTTPUpstream, proxies ProxyRepository) gov.ClientFactory {
	return func(ctx context.Context, site gov.Site) (gov.HTTPDoer, error) {
		proxyURL, err := governanceProxyURL(ctx, proxies, site)
		if err != nil {
			return nil, err
		}
		if expected, pinned := gov.BrowserAuthorizationProxy(ctx); pinned && expected != proxyURL {
			return nil, gov.ErrConflict
		}
		return governanceHTTPClient{upstream: upstream, proxy: proxyURL}, nil
	}
}
func ProvideUpstreamGovernanceService(db *sql.DB, admin AdminService, upstream HTTPUpstream, proxies ProxyRepository, cipher SecretEncryptor, cfg *config.Config, email *EmailService, settings SettingRepository, users UserRepository) *gov.Service {
	localAccounts := &governanceLocalAccounts{db: db, admin: admin}
	svc := gov.NewService(gov.NewSQLStore(db), gov.NewConnector(governanceClientFactory(upstream, proxies)), localAccounts, cipher, cfg != nil && cfg.Totp.EncryptionKeyConfigured)
	if cfg != nil && cfg.Server.Port > 0 {
		if runner, err := gov.NewLocalGatewayModelRunner(fmt.Sprintf("http://127.0.0.1:%d", cfg.Server.Port)); err == nil {
			svc.SetLocalModelRunner(runner)
		}
	}
	if pricingStore, ok := gov.NewSQLStore(db).(gov.PricingPersistence); ok {
		svc.SetPricingCoordinator(gov.NewPricingCoordinator(pricingStore))
	}
	notifier := &governanceBalanceNotifier{settings: settings, admins: users}
	if email != nil {
		notifier.mail = email
	}
	svc.SetBalanceNotifier(notifier)
	svc.SetKeyNotifier(notifier)
	svc.SetModelNotifier(notifier)
	svc.SetChangeNotifier(notifier)
	configureGovernanceBrowser(svc, proxies)
	svc.Start()
	return svc
}
