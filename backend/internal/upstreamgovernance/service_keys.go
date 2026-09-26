package upstreamgovernance

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ManagedKey records remote ownership independently of any local import.
// KeyCipher and OwnerUserID never appear in API metadata or audit output.
type ManagedKey struct {
	ID            int64     `json:"id"`
	SiteID        int64     `json:"site_id"`
	RemoteGroupID string    `json:"remote_group_id"`
	Platform      string    `json:"platform"`
	RemoteKeyID   string    `json:"remote_key_id"`
	Marker        string    `json:"marker"`
	HasKey        bool      `json:"has_key"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	OwnerUserID   int64     `json:"-"`
	KeyCipher     string    `json:"-"`
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

// Grok's gateway exposes the OpenAI wire protocol. Unknown/composite groups
// need the administrator's explicit transport; other unsupported labels are
// never silently treated as OpenAI.
func compatibleTransport(remote, selected string) bool {
	if !validTransport(selected) {
		return false
	}
	switch remote {
	case "", "unknown", "composite":
		return true
	case "grok":
		return selected == "openai"
	default:
		return remote == selected
	}
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
	session, err := s.session(*site)
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
		if !ok || !compatibleTransport(group.Platform, selected.Platform) || seen[key] {
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
	for _, selected := range input.Selections {
		group := groups[selected.RemoteGroupID]
		stable := marker(siteID, group.ID, selected.Platform)
		binding := byMarker[stable]
		item := KeyItemResult{RemoteGroupID: group.ID, Platform: selected.Platform, Status: "failed"}
		record, key, reused, itemErr := s.ensureManagedKey(ctx, *site, session, group, selected.Platform, managed[stable], &binding)
		if itemErr != nil {
			item.Error = ErrorCode(itemErr)
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
		if err := s.store.SaveManagedKey(ctx, managed); err != nil {
			return nil, RemoteKey{}, false, err
		}
	}
	key, err := s.connector.EnsureKey(ctx, site, session, group, stable)
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
