package upstreamgovernance

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Local model monitoring reuses the durable model-monitoring tables, but uses
// a hidden per-administrator scope row instead of pretending that a local
// group is an upstream site. The marker is kept in login_cipher, which is
// already excluded from every public Site JSON projection.
const (
	localModelSiteStatus  = "local_model"
	localModelOwnerPrefix = "local-model-owner:"
)

var localModelSiteMu sync.Mutex

// The optional store capability keeps memory/legacy ports compatible while
// SQL installations coordinate first creation across server processes.
type localModelWorkspaceStore interface {
	ensureLocalModelSite(context.Context, int64, time.Time) (*Site, error)
}

func localModelOwner(site Site) (int64, bool) {
	if site.Status != localModelSiteStatus || !strings.HasPrefix(site.LoginCipher, localModelOwnerPrefix) {
		return 0, false
	}
	owner, err := strconv.ParseInt(strings.TrimPrefix(site.LoginCipher, localModelOwnerPrefix), 10, 64)
	return owner, err == nil && owner > 0
}

func isLocalModelSite(site Site) bool {
	_, ok := localModelOwner(site)
	return ok
}

// remoteSiteLock is the common guard for operations that talk to or mutate an
// upstream site. A local workspace may use the same durable model tables, but
// it must never enter the remote collection, import, key, or reconciliation
// paths.
func (s *Service) remoteSiteLock(ctx context.Context, siteID int64) (*Site, func(), error) {
	site, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return nil, nil, err
	}
	if isLocalModelSite(*site) {
		release()
		return nil, nil, ErrInvalid
	}
	return site, release, nil
}

// EnsureLocalModelSite returns the hidden durable scope used to persist local
// model-monitoring runs for one administrator. It is deliberately lazy so
// installations that never use local monitoring receive no extra row.
func (s *Service) EnsureLocalModelSite(ctx context.Context, ownerID int64) (*Site, error) {
	if s == nil || s.store == nil || ownerID <= 0 {
		return nil, ErrInvalid
	}
	if store, ok := s.store.(localModelWorkspaceStore); ok {
		return store.ensureLocalModelSite(ctx, ownerID, s.now())
	}
	localModelSiteMu.Lock()
	defer localModelSiteMu.Unlock()

	sites, err := s.store.ListSites(ctx)
	if err != nil {
		return nil, err
	}
	for i := range sites {
		if owner, ok := localModelOwner(sites[i]); ok && owner == ownerID {
			return &sites[i], nil
		}
	}

	site := newLocalModelSite(ownerID, s.now())
	if err := s.store.CreateSite(ctx, site); err != nil {
		return nil, err
	}
	return site, nil
}

func newLocalModelSite(ownerID int64, now time.Time) *Site {
	return &Site{
		Name:            "本地分组检测",
		Platform:        "sub2api",
		BaseURL:         "http://127.0.0.1",
		Enabled:         true,
		IntervalMinutes: 1440,
		Status:          localModelSiteStatus,
		NextSyncAt:      now,
		CreatedAt:       now,
		UpdatedAt:       now,
		LoginCipher:     localModelOwnerPrefix + strconv.FormatInt(ownerID, 10),
	}
}

func (s *sqlStore) ensureLocalModelSite(ctx context.Context, ownerID int64, now time.Time) (*Site, error) {
	if s == nil || s.db == nil || ownerID <= 0 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	marker := localModelOwnerPrefix + strconv.FormatInt(ownerID, 10)
	// This lock includes the absent-row case, so two server instances cannot
	// create separate durable scopes for the same administrator. A hash
	// collision only serializes unrelated owners and cannot mix their data.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, marker); err != nil {
		return nil, err
	}
	site, err := scanSite(tx.QueryRowContext(ctx, `SELECT `+siteColumns+` FROM upstream_governance_sites WHERE status=$1 AND login_cipher=$2 ORDER BY id LIMIT 1`, localModelSiteStatus, marker))
	if errors.Is(err, ErrNotFound) {
		site = newLocalModelSite(ownerID, now)
		// Only columns shared by the supported governance schemas are required;
		// the local scope never uses remote collection or fast observation.
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_governance_sites(name,platform,base_url,enabled,interval_minutes,status,next_sync_at,login_cipher) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,version,created_at,updated_at`, site.Name, site.Platform, site.BaseURL, site.Enabled, site.IntervalMinutes, site.Status, site.NextSyncAt, site.LoginCipher).Scan(&site.ID, &site.Version, &site.CreatedAt, &site.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return site, nil
}

// AuthorizeModelSite protects the hidden local scope while leaving existing
// upstream governance sites shared between administrators as before.
func (s *Service) AuthorizeModelSite(ctx context.Context, siteID, ownerID int64) error {
	if s == nil || s.store == nil || siteID <= 0 || ownerID <= 0 {
		return ErrInvalid
	}
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return err
	}
	if localOwner, ok := localModelOwner(*site); ok && localOwner != ownerID {
		return ErrNotFound
	}
	return nil
}
