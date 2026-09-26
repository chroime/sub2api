package upstreamgovernance

import (
	"context"
	"database/sql"
	"errors"
)

const managedKeyColumns = `id,site_id,remote_group_id,platform,remote_key_id,marker,owner_user_id,key_cipher,created_at,updated_at`

func scanManagedKey(row rowScanner) (*ManagedKey, error) {
	var key ManagedKey
	err := row.Scan(&key.ID, &key.SiteID, &key.RemoteGroupID, &key.Platform, &key.RemoteKeyID, &key.Marker, &key.OwnerUserID, &key.KeyCipher, &key.CreatedAt, &key.UpdatedAt)
	if err != nil {
		return nil, storeError(err)
	}
	key.HasKey = key.KeyCipher != ""
	return &key, nil
}

func (s *sqlStore) ListManagedKeys(ctx context.Context, siteID int64) ([]ManagedKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+managedKeyColumns+` FROM upstream_governance_keys WHERE site_id=$1 ORDER BY id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []ManagedKey{}
	for rows.Next() {
		key, err := scanManagedKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *key)
	}
	return keys, rows.Err()
}

func (s *sqlStore) GetManagedKey(ctx context.Context, siteID, keyID int64) (*ManagedKey, error) {
	return scanManagedKey(s.db.QueryRowContext(ctx, `SELECT `+managedKeyColumns+` FROM upstream_governance_keys WHERE site_id=$1 AND id=$2`, siteID, keyID))
}

func (s *sqlStore) SaveManagedKey(ctx context.Context, key *ManagedKey) error {
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO upstream_governance_keys (site_id,remote_group_id,platform,remote_key_id,marker,owner_user_id,key_cipher) VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (site_id,remote_group_id,platform) DO UPDATE SET remote_key_id=EXCLUDED.remote_key_id,key_cipher=EXCLUDED.key_cipher,updated_at=NOW()
WHERE upstream_governance_keys.marker=EXCLUDED.marker AND upstream_governance_keys.owner_user_id=EXCLUDED.owner_user_id
AND (upstream_governance_keys.key_cipher='' OR (upstream_governance_keys.key_cipher=EXCLUDED.key_cipher AND upstream_governance_keys.remote_key_id=EXCLUDED.remote_key_id))
RETURNING id,created_at,updated_at`, key.SiteID, key.RemoteGroupID, key.Platform, key.RemoteKeyID, key.Marker, key.OwnerUserID, key.KeyCipher).Scan(&id, &key.CreatedAt, &key.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err == nil {
		key.ID = id
		key.HasKey = key.KeyCipher != ""
	}
	return err
}
