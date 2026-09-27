package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const managedKeyColumns = `id,site_id,remote_group_id,platform,remote_key_id,marker,owner_user_id,key_cipher,created_at,updated_at,creation_plan`

func scanManagedKey(row rowScanner) (*ManagedKey, error) {
	var key ManagedKey
	var planJSON []byte
	err := row.Scan(&key.ID, &key.SiteID, &key.RemoteGroupID, &key.Platform, &key.RemoteKeyID, &key.Marker, &key.OwnerUserID, &key.KeyCipher, &key.CreatedAt, &key.UpdatedAt, &planJSON)
	if err != nil {
		return nil, storeError(err)
	}
	if len(planJSON) > 0 {
		if err := json.Unmarshal(planJSON, &key.CreationPlan); err != nil || key.CreationPlan == nil {
			return nil, ErrInvalid
		}
		if err := validateStoredKeyCreationPlan(key.CreationPlan); err != nil {
			return nil, err
		}
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
	var planJSON any
	if key.CreationPlan != nil {
		if err := validateStoredKeyCreationPlan(key.CreationPlan); err != nil {
			return err
		}
		if key.KeyCipher != "" && key.CreationPlan.ExistingIDs == nil {
			return ErrInvalid
		}
		raw, err := json.Marshal(key.CreationPlan)
		if err != nil {
			return ErrInvalid
		}
		planJSON = string(raw)
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO upstream_governance_keys (site_id,remote_group_id,platform,remote_key_id,marker,owner_user_id,key_cipher,creation_plan) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (site_id,remote_group_id,platform) DO UPDATE SET remote_key_id=EXCLUDED.remote_key_id,key_cipher=EXCLUDED.key_cipher,creation_plan=EXCLUDED.creation_plan,updated_at=NOW()
WHERE upstream_governance_keys.marker=EXCLUDED.marker AND upstream_governance_keys.owner_user_id=EXCLUDED.owner_user_id
AND (upstream_governance_keys.key_cipher='' OR (upstream_governance_keys.key_cipher=EXCLUDED.key_cipher AND upstream_governance_keys.remote_key_id=EXCLUDED.remote_key_id))
AND (upstream_governance_keys.creation_plan IS NOT DISTINCT FROM EXCLUDED.creation_plan OR (
 upstream_governance_keys.key_cipher='' AND upstream_governance_keys.creation_plan IS NOT NULL AND EXCLUDED.creation_plan IS NOT NULL
 AND upstream_governance_keys.creation_plan->>'name'=EXCLUDED.creation_plan->>'name'
 AND upstream_governance_keys.creation_plan->'existing_ids'='null'::jsonb
 AND jsonb_typeof(EXCLUDED.creation_plan->'existing_ids')='array'
))
RETURNING id,created_at,updated_at`, key.SiteID, key.RemoteGroupID, key.Platform, key.RemoteKeyID, key.Marker, key.OwnerUserID, key.KeyCipher, planJSON).Scan(&id, &key.CreatedAt, &key.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err == nil {
		key.ID = id
		key.HasKey = key.KeyCipher != ""
	}
	return err
}

func validateStoredKeyCreationPlan(plan *KeyCreationPlan) error {
	if !validKeyDisplayName(plan.Name) || len(plan.ExistingIDs) > 2000 {
		return ErrInvalid
	}
	seen := make(map[int64]bool, len(plan.ExistingIDs))
	for _, id := range plan.ExistingIDs {
		if id <= 0 || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}
