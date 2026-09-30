package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

const managedKeyColumns = `id,site_id,remote_group_id,platform,remote_key_id,marker,owner_user_id,key_cipher,created_at,updated_at,creation_plan,key_health`

func scanManagedKey(row rowScanner) (*ManagedKey, error) {
	var key ManagedKey
	var planJSON []byte
	var healthJSON []byte
	err := row.Scan(&key.ID, &key.SiteID, &key.RemoteGroupID, &key.Platform, &key.RemoteKeyID, &key.Marker, &key.OwnerUserID, &key.KeyCipher, &key.CreatedAt, &key.UpdatedAt, &planJSON, &healthJSON)
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
	if err := json.Unmarshal(healthJSON, &key.Health); err != nil || validateKeyHealth(key.Health) != nil {
		return nil, ErrInvalid
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

// SaveKeyHealth updates only observation data for the same completed key.
// A changed site, owner, marker, remote ID or ciphertext fails closed.
func (s *sqlStore) SaveKeyHealth(ctx context.Context, key ManagedKey, health KeyHealth) error {
	if key.ID <= 0 || key.SiteID <= 0 || key.RemoteGroupID == "" || key.Platform == "" || key.RemoteKeyID == "" || key.Marker == "" || key.OwnerUserID <= 0 || key.KeyCipher == "" {
		return ErrInvalid
	}
	if err := validateKeyHealth(health); err != nil {
		return err
	}
	raw, err := json.Marshal(health)
	if err != nil {
		return ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_keys SET key_health=$1::jsonb
WHERE id=$2 AND site_id=$3 AND remote_group_id=$4 AND platform=$5 AND remote_key_id=$6 AND marker=$7 AND owner_user_id=$8 AND key_cipher=$9 AND key_cipher<>''`,
		string(raw), key.ID, key.SiteID, key.RemoteGroupID, key.Platform, key.RemoteKeyID, key.Marker, key.OwnerUserID, key.KeyCipher)
	return affected(r, err, ErrConflict)
}

func validateKeyHealth(health KeyHealth) error {
	switch health.Status {
	case KeyHealthUnknown, KeyHealthPresent, KeyHealthSuspectedMissing, KeyHealthConfirmedMissing, KeyHealthGroupChanged:
	default:
		return ErrInvalid
	}
	if health.MissingCount < 0 || len(health.ErrorCode) > 64 || strings.IndexFunc(health.ErrorCode, func(r rune) bool {
		return r != '_' && !unicode.IsLower(r) && !unicode.IsDigit(r)
	}) >= 0 {
		return ErrInvalid
	}
	return nil
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
