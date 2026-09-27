package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// KeyCreationPlan freezes the display name and the matching keys that existed
// before creation. A nil inventory means no remote creation may have started.
type KeyCreationPlan struct {
	Name        string  `json:"name"`
	ExistingIDs []int64 `json:"existing_ids"`
}

// ManagedKey records remote ownership independently of any local import.
// Private ownership, creation state and ciphertext never appear in API metadata.
type ManagedKey struct {
	ID            int64            `json:"id"`
	SiteID        int64            `json:"site_id"`
	RemoteGroupID string           `json:"remote_group_id"`
	Platform      string           `json:"platform"`
	RemoteKeyID   string           `json:"remote_key_id"`
	Marker        string           `json:"marker"`
	HasKey        bool             `json:"has_key"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	OwnerUserID   int64            `json:"-"`
	KeyCipher     string           `json:"-"`
	CreationPlan  *KeyCreationPlan `json:"-"`
}

type KeySelection struct {
	RemoteGroupID string `json:"remote_group_id"`
	Platform      string `json:"platform"`
}

type CreateKeysInput struct {
	SnapshotID int64          `json:"snapshot_id"`
	Selections []KeySelection `json:"selections"`
}

type KeyItemResult struct {
	RemoteGroupID string      `json:"remote_group_id"`
	Platform      string      `json:"platform"`
	Status        string      `json:"status"`
	ManagedKey    *ManagedKey `json:"managed_key,omitempty"`
	Key           string      `json:"key,omitempty"`
	Error         string      `json:"error,omitempty"`
}

type CreateKeysResult struct {
	Items []KeyItemResult `json:"items"`
}

type RevealKeyResult struct {
	ManagedKey *ManagedKey `json:"managed_key"`
	Key        string      `json:"key"`
}

func (s *Service) Keys(ctx context.Context, siteID int64) ([]ManagedKey, error) {
	if _, err := s.store.GetSite(ctx, siteID); err != nil {
		return nil, err
	}
	keys, err := s.store.ListManagedKeys(ctx, siteID)
	if keys == nil && err == nil {
		keys = []ManagedKey{}
	}
	return keys, err
}

func (s *Service) RevealKey(ctx context.Context, siteID, keyID int64) (*RevealKeyResult, error) {
	if !s.durableKey || s.cipher == nil {
		return nil, ErrEncryption
	}
	_, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	managed, err := s.store.GetManagedKey(ctx, siteID, keyID)
	if err != nil {
		return nil, err
	}
	if managed.KeyCipher == "" {
		return nil, ErrNotFound
	}
	key, err := s.decryptManagedKey(managed.KeyCipher)
	if err != nil {
		return nil, err
	}
	return &RevealKeyResult{ManagedKey: managed, Key: key.Key}, nil
}

func (s *Service) CreateKeys(ctx context.Context, siteID int64, input CreateKeysInput) (*CreateKeysResult, error) {
	if input.SnapshotID <= 0 || len(input.Selections) == 0 || len(input.Selections) > 100 {
		return nil, ErrInvalid
	}
	free, err := s.remoteSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer free()
	site, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	session, err := s.managementSessionLocked(ctx, site, false)
	if err != nil {
		return nil, err
	}
	if session.UserID <= 0 {
		return nil, ErrReauth
	}
	snapshot, err := s.store.LatestSnapshot(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if snapshot.ID != input.SnapshotID || snapshot.SiteVersion != site.Version {
		return nil, ErrConflict
	}
	groups := make(map[string]RemoteGroup, len(snapshot.Catalog.Groups))
	for _, group := range snapshot.Catalog.Groups {
		groups[group.ID] = group
	}
	seen := map[string]bool{}
	for _, selected := range input.Selections {
		group, ok := groups[selected.RemoteGroupID]
		key := marker(siteID, selected.RemoteGroupID, selected.Platform)
		if !ok || !validSiteTransport(site.Platform, selected.Platform) || !compatibleTransport(group.Platform, selected.Platform) || seen[key] {
			return nil, ErrInvalid
		}
		seen[key] = true
	}
	managed, err := s.managedKeysByMarker(ctx, siteID)
	if err != nil {
		return nil, err
	}
	bindings, err := s.store.ListBindings(ctx, siteID)
	if err != nil {
		return nil, err
	}
	byMarker := map[string]Binding{}
	for _, binding := range bindings {
		byMarker[binding.Marker] = binding
	}
	result := &CreateKeysResult{Items: []KeyItemResult{}}
	unauthorized := false
	for _, selected := range input.Selections {
		group := groups[selected.RemoteGroupID]
		stable := marker(siteID, group.ID, selected.Platform)
		binding := byMarker[stable]
		item := KeyItemResult{RemoteGroupID: group.ID, Platform: selected.Platform, Status: "failed"}
		if unauthorized {
			item.Error = "reauth_required"
			result.Items = append(result.Items, item)
			continue
		}
		record, key, reused, itemErr := s.ensureManagedKey(ctx, *site, session, group, selected.Platform, managed[stable], &binding)
		if itemErr != nil {
			item.Error = ErrorCode(itemErr)
			if errors.Is(itemErr, ErrReauth) {
				unauthorized = true
				if authErr := s.requireAuthorizationLocked(ctx, site, session); !errors.Is(authErr, ErrReauth) {
					return nil, authErr
				}
			}
		} else {
			item.Status = "created"
			if reused {
				item.Status = "reused"
			}
			item.ManagedKey = record
			item.Key = key.Key
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Service) managedKeysByMarker(ctx context.Context, siteID int64) (map[string]*ManagedKey, error) {
	keys, err := s.store.ListManagedKeys(ctx, siteID)
	if err != nil {
		return nil, err
	}
	byMarker := make(map[string]*ManagedKey, len(keys))
	for i := range keys {
		byMarker[keys[i].Marker] = &keys[i]
	}
	return byMarker, nil
}

func (s *Service) decryptManagedKey(ciphertext string) (RemoteKey, error) {
	var key RemoteKey
	if !s.durableKey || s.cipher == nil {
		return key, ErrEncryption
	}
	raw, err := s.cipher.Decrypt(ciphertext)
	if err != nil || json.Unmarshal([]byte(raw), &key) != nil || strings.TrimSpace(key.Key) == "" {
		return RemoteKey{}, ErrReauth
	}
	return key, nil
}

// The caller holds the site lock. A pending record is persisted before any
// remote POST, so an uncertain request cannot lose its user/origin ownership.
func (s *Service) ensureManagedKey(ctx context.Context, site Site, session Session, group RemoteGroup, transport string, managed *ManagedKey, binding *Binding) (*ManagedKey, RemoteKey, bool, error) {
	if session.UserID <= 0 {
		return nil, RemoteKey{}, false, ErrReauth
	}
	stable := marker(site.ID, group.ID, transport)
	newManaged := managed == nil
	if managed == nil {
		managed = &ManagedKey{SiteID: site.ID, RemoteGroupID: group.ID, Platform: transport, Marker: stable, OwnerUserID: session.UserID, CreatedAt: s.now(), UpdatedAt: s.now()}
	}
	if managed.SiteID != site.ID || managed.OwnerUserID != session.UserID || managed.Marker != stable || managed.RemoteGroupID != group.ID || managed.Platform != transport {
		return nil, RemoteKey{}, false, ErrConflict
	}
	if managed.KeyCipher != "" {
		key, err := s.decryptManagedKey(managed.KeyCipher)
		if err != nil {
			return nil, RemoteKey{}, false, err
		}
		if key.ID != managed.RemoteKeyID {
			return nil, RemoteKey{}, false, ErrConflict
		}
		if binding != nil && binding.KeyCipher != "" {
			previous, err := s.decryptManagedKey(binding.KeyCipher)
			if err != nil {
				return nil, RemoteKey{}, false, err
			}
			if previous != key {
				return nil, RemoteKey{}, false, ErrConflict
			}
		}
		managed.HasKey = true
		return managed, key, true, nil
	}
	if binding != nil && binding.KeyCipher != "" {
		if binding.SiteID != site.ID || binding.RemoteGroupID != group.ID || binding.Platform != transport || binding.Marker != stable {
			return nil, RemoteKey{}, false, ErrConflict
		}
		key, err := s.decryptManagedKey(binding.KeyCipher)
		if err != nil {
			return nil, RemoteKey{}, false, err
		}
		managed.KeyCipher = binding.KeyCipher
		managed.RemoteKeyID = key.ID
		managed.HasKey = true
		if err = s.store.SaveManagedKey(ctx, managed); err != nil {
			return nil, RemoteKey{}, false, err
		}
		return managed, key, true, nil
	}
	if managed.ID == 0 {
		if newManaged {
			managed.CreationPlan = &KeyCreationPlan{Name: managedKeyName(group, managed.CreatedAt, site.Platform)}
		}
		if err := s.store.SaveManagedKey(ctx, managed); err != nil {
			return nil, RemoteKey{}, false, err
		}
	}
	if managed.CreationPlan != nil && managed.CreationPlan.ExistingIDs == nil {
		existingIDs, err := s.connector.PrepareKey(ctx, site, session, group, managed.CreationPlan.Name)
		if err != nil {
			return nil, RemoteKey{}, false, err
		}
		// Keep an empty inventory distinct from an inventory not yet recorded.
		// Persist it before any POST so retries cannot adopt a pre-existing key.
		managed.CreationPlan = &KeyCreationPlan{Name: managed.CreationPlan.Name, ExistingIDs: append([]int64{}, existingIDs...)}
		if err := s.store.SaveManagedKey(ctx, managed); err != nil {
			return nil, RemoteKey{}, false, err
		}
	}
	plan, err := s.keyCreationAttemptPlan(ctx, managed)
	if err != nil {
		return nil, RemoteKey{}, false, err
	}
	key, err := s.connector.EnsureKey(ctx, site, session, group, stable, plan)
	if err != nil {
		return nil, RemoteKey{}, false, err
	}
	if strings.TrimSpace(key.Key) == "" || key.ID == "" {
		return nil, RemoteKey{}, false, ErrUnsupported
	}
	raw, err := json.Marshal(key)
	if err != nil {
		return nil, RemoteKey{}, false, ErrUnsupported
	}
	managed.KeyCipher, err = s.cipher.Encrypt(string(raw))
	if err != nil {
		return nil, RemoteKey{}, false, ErrEncryption
	}
	managed.RemoteKeyID = key.ID
	managed.HasKey = true
	if err = s.store.SaveManagedKey(ctx, managed); err != nil {
		return nil, RemoteKey{}, false, err
	}
	return managed, key, false, nil
}

func (s *Service) keyCreationAttemptPlan(ctx context.Context, managed *ManagedKey) (*KeyCreationPlan, error) {
	if managed.CreationPlan == nil {
		return nil, nil
	}
	plan := &KeyCreationPlan{Name: managed.CreationPlan.Name, ExistingIDs: append([]int64{}, managed.CreationPlan.ExistingIDs...)}
	keys, err := s.store.ListManagedKeys(ctx, managed.SiteID)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(plan.ExistingIDs))
	for _, id := range plan.ExistingIDs {
		seen[id] = true
	}
	// Another protocol may have completed after this pending operation began.
	// Exclude its owned key only for this attempt; the persisted inventory and
	// request name remain fixed across retries and process restarts.
	for _, other := range keys {
		if other.Marker == managed.Marker || other.RemoteGroupID != managed.RemoteGroupID || other.KeyCipher == "" {
			continue
		}
		id, err := strconv.ParseInt(other.RemoteKeyID, 10, 64)
		if err == nil && id > 0 && !seen[id] {
			plan.ExistingIDs = append(plan.ExistingIDs, id)
			seen[id] = true
		}
	}
	return plan, nil
}
