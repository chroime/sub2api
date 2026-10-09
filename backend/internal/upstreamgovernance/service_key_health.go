package upstreamgovernance

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUpstreamKeyMissing      = errors.New("managed upstream key is missing")
	ErrUpstreamKeyUnverifiable = errors.New("managed upstream key cannot be verified")
)

type KeyIssueCounter interface {
	CountKeyIssues(context.Context) (map[int64]int, error)
}

// inventoryForSite never creates keys and accepts only a complete inventory
// belonging to the account that originally created the managed records.
func (s *Service) inventoryForSite(ctx context.Context, site Site, session Session, keys []ManagedKey) (map[string]string, error) {
	if session.UserID <= 0 {
		return nil, ErrReauth
	}
	for _, key := range keys {
		if key.KeyCipher != "" && (key.OwnerUserID != session.UserID || key.SiteID != site.ID) {
			return nil, ErrConflict
		}
	}
	reader, ok := s.connector.(KeyInventoryReader)
	if !ok {
		return nil, ErrUpstreamKeyUnverifiable
	}
	remote, err := reader.ListKeyInventory(ctx, site, session)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(remote))
	for _, item := range remote {
		if item.ID == "" {
			return nil, ErrUpstreamKeyUnverifiable
		}
		if _, exists := result[item.ID]; exists {
			return nil, ErrUpstreamKeyUnverifiable
		}
		result[item.ID] = item.GroupID
	}
	for _, key := range keys {
		if key.KeyCipher != "" && key.RemoteKeyID != "" {
			if group, exists := result[key.RemoteKeyID]; exists && group == "" {
				return nil, ErrUpstreamKeyUnverifiable
			}
		}
	}
	return result, nil
}

func keyRemoteID(s *Service, managed *ManagedKey, binding *Binding) (string, error) {
	if managed != nil && managed.KeyCipher != "" {
		return managed.RemoteKeyID, nil
	}
	if binding != nil && binding.KeyCipher != "" {
		key, err := s.decryptManagedKey(binding.KeyCipher)
		if err != nil {
			return "", err
		}
		return key.ID, nil
	}
	return "", nil
}

func checkedKeyInventory(inventory map[string]string, remoteID, groupID string) error {
	if remoteID == "" {
		return nil
	}
	if inventory == nil {
		return ErrUpstreamKeyUnverifiable
	}
	if observed, exists := inventory[remoteID]; exists && observed == "" {
		return ErrUpstreamKeyUnverifiable
	} else if !exists || observed != groupID {
		return ErrUpstreamKeyMissing
	}
	return nil
}

func inventoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrReauth) {
		return ErrReauth
	}
	return ErrUpstreamKeyUnverifiable
}

// A completed import receipt is historical. Read-only verification prevents
// replay from describing a now-deleted key as currently usable.
func (s *Service) verifyAppliedPreviewKeys(ctx context.Context, site Site, preview *Preview) error {
	session, err := s.managementSessionLocked(ctx, &site, false)
	if err != nil {
		return err
	}
	allKeys, err := s.store.ListManagedKeys(ctx, site.ID)
	if err != nil {
		return err
	}
	bindings, err := s.store.ListBindings(ctx, site.ID)
	if err != nil {
		return err
	}
	managedByMarker := make(map[string]*ManagedKey, len(allKeys))
	for i := range allKeys {
		managedByMarker[allKeys[i].Marker] = &allKeys[i]
	}
	bindingByMarker := make(map[string]*Binding, len(bindings))
	for i := range bindings {
		bindingByMarker[bindings[i].Marker] = &bindings[i]
	}
	inventory, err := s.inventoryForSite(ctx, site, session, allKeys)
	if err != nil {
		if errors.Is(err, ErrReauth) {
			if authErr := s.requireAuthorizationLocked(ctx, &site, session); !errors.Is(authErr, ErrReauth) {
				return authErr
			}
		}
		return inventoryError(err)
	}
	for _, row := range preview.Rows {
		id, keyErr := keyRemoteID(s, managedByMarker[row.Marker], bindingByMarker[row.Marker])
		if keyErr != nil || id == "" {
			return ErrUpstreamKeyUnverifiable
		}
		if err = checkedKeyInventory(inventory, id, row.RemoteGroup.ID); err != nil {
			return err
		}
	}
	return nil
}

// AuditKeys is an explicit read-only verification. It never reveals a secret
// or creates a replacement key. The returned records are administrator DTOs.
func (s *Service) AuditKeys(ctx context.Context, siteID int64) ([]ManagedKey, error) {
	free, err := s.remoteSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer free()
	site, release, err := s.remoteSiteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err = s.auditManagedKeysLocked(checkCtx, *site); err != nil {
		return nil, err
	}
	return s.store.ListManagedKeys(ctx, siteID)
}

func (s *Service) keyAuditDue(ctx context.Context, siteID int64, now time.Time) (bool, error) {
	keys, err := s.store.ListManagedKeys(ctx, siteID)
	if err != nil {
		return false, err
	}
	for _, key := range keys {
		if key.Health.NextCheckAt != nil && !now.Before(*key.Health.NextCheckAt) {
			return true, nil
		}
	}
	return false, nil
}

// The caller owns the site lock. Failed or incomplete remote inventory never
// advances missing confirmations or changes a successful catalog snapshot.
func (s *Service) auditManagedKeysLocked(ctx context.Context, site Site) error {
	store, ok := s.store.(KeyHealthStore)
	if !ok {
		return ErrUnsupported
	}
	keys, err := s.store.ListManagedKeys(ctx, site.ID)
	if err != nil || len(keys) == 0 {
		return err
	}
	session, err := s.managementSessionLocked(ctx, &site, false)
	if err != nil {
		return err
	}
	inventory, inventoryErr := s.inventoryForSite(ctx, site, session, keys)
	now := s.now().UTC()
	type pendingNotification struct {
		key    ManagedKey
		health KeyHealth
	}
	pending := make([]pendingNotification, 0, len(keys))
	for _, key := range keys {
		if key.KeyCipher == "" {
			continue
		}
		health := key.Health
		if health.Status == "" {
			health.Status = KeyHealthUnknown
		}
		notificationDue := health.NotificationKind != "" && health.NotificationStatus != "sent" &&
			(health.NextCheckAt == nil || !now.Before(*health.NextCheckAt))
		if inventoryErr != nil {
			// Keep the last proven state. In particular a timeout must not
			// undo a confirmed incident or become another missing vote.
			health.ErrorCode = ErrorCode(inventoryErr)
			next := now.Add(5 * time.Minute)
			health.NextCheckAt = &next
		} else {
			health.LastCheckedAt = &now
			health.ErrorCode = ""
			group, visible := inventory[key.RemoteKeyID]
			if visible && group == key.RemoteGroupID {
				wasMissing := health.Status == KeyHealthConfirmedMissing || health.Status == KeyHealthGroupChanged && health.MissingCount >= 2
				health.Status = KeyHealthPresent
				health.LastVerifiedAt = &now
				health.MissingCount = 0
				health.FirstMissingAt = nil
				health.NextCheckAt = nil
				if wasMissing {
					health.NotificationKind = "recovered"
					health.NotificationStatus = "pending"
					health.NotificationReservedAt = nil
					health.NotificationSentAt = nil
					health.NotificationRecipients = nil
				}
			} else {
				wasConfirmed := health.MissingCount >= 2 && (health.Status == KeyHealthConfirmedMissing || health.Status == KeyHealthGroupChanged)
				if health.MissingCount == 0 || health.FirstMissingAt == nil {
					health.MissingCount = 1
					health.FirstMissingAt = &now
					next := now.Add(time.Minute)
					health.NextCheckAt = &next
				} else if health.NextCheckAt != nil && !now.Before(*health.NextCheckAt) {
					health.MissingCount++
					health.NextCheckAt = nil
				}
				if visible {
					health.Status = KeyHealthGroupChanged
				} else if health.MissingCount >= 2 {
					health.Status = KeyHealthConfirmedMissing
				} else {
					health.Status = KeyHealthSuspectedMissing
				}
				if !wasConfirmed && health.MissingCount >= 2 {
					health.NotificationKind = "missing"
					health.NotificationStatus = "pending"
					health.NotificationReservedAt = nil
					health.NotificationSentAt = nil
					health.NotificationRecipients = nil
				}
			}
		}
		if err = store.SaveKeyHealth(ctx, key, health); err != nil {
			return err
		}
		if inventoryErr != nil && notificationDue {
			pending = append(pending, pendingNotification{key: key, health: health})
		}
		if inventoryErr == nil {
			if err = s.protectKeyAccount(ctx, site, key, health); err != nil {
				health.ProtectionError = ErrorCode(err)
				next := now.Add(time.Minute)
				health.NextCheckAt = &next
				persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				persistErr := store.SaveKeyHealth(persistCtx, key, health)
				cancel()
				if persistErr != nil {
					return persistErr
				}
				return err
			}
			if health.ProtectionError != "" {
				health.ProtectionError = ""
				health.NextCheckAt = nil
				if err = store.SaveKeyHealth(ctx, key, health); err != nil {
					return err
				}
			}
			// A confirmed remote deletion is already paused above. Freeze a
			// reviewed, idempotent repair intent while the site lock is held so
			// scheduled audits can hand the operation to the existing repair
			// state machine without ever issuing a remote create here.
			// A key moved to another upstream group is a distinct manual
			// reconciliation case. Only a confirmed absence may enqueue a
			// replacement intent; never create a new key while the old key is
			// still visible under a different group.
			confirmedMissing := health.Status == KeyHealthConfirmedMissing
			if confirmedMissing {
				repairKey := key
				repairKey.Health = health
				_, created, repairErr := s.ensureKeyRepairIntentLocked(ctx, site, repairKey)
				if repairErr != nil {
					code := "key_repair_pending"
					kind := "key_repair_pending"
					if errors.Is(repairErr, ErrUnsupported) {
						code = "key_repair_manual_required"
						kind = "key_repair_manual_required"
					}
					if key.Health.ErrorCode != code {
						if eventErr := s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: kind, Resource: key.Marker, Before: key.RemoteKeyID, After: code, CreatedAt: now}); eventErr != nil {
							log.Printf("[UpstreamGovernance] key repair intent: %s", ErrorCode(eventErr))
						}
					}
					health.ErrorCode = code
					if health.NextCheckAt == nil {
						next := now.Add(5 * time.Minute)
						health.NextCheckAt = &next
					}
					if err = store.SaveKeyHealth(ctx, key, health); err != nil {
						return err
					}
				} else if created {
					if eventErr := s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "key_repair_pending", Resource: key.Marker, Before: key.RemoteKeyID, After: "prepared", CreatedAt: now}); eventErr != nil {
						log.Printf("[UpstreamGovernance] key repair intent event: %s", ErrorCode(eventErr))
					}
				}
			}
			if key.Health.Status != health.Status || key.Health.MissingCount < 2 && health.MissingCount >= 2 {
				kind := "key_missing_suspected"
				if health.Status == KeyHealthConfirmedMissing || health.Status == KeyHealthGroupChanged && health.MissingCount >= 2 {
					kind = "key_missing_confirmed"
				} else if health.Status == KeyHealthPresent {
					kind = "key_recovered"
				}
				if eventErr := s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: kind, Resource: key.Marker, Before: key.Health.Status, After: health.Status, CreatedAt: now}); eventErr != nil {
					log.Printf("[UpstreamGovernance] key health event: %s", ErrorCode(eventErr))
				}
			}
			if health.NotificationKind != "" && health.NotificationStatus != "sent" {
				if health.NextCheckAt == nil {
					next := now.Add(15 * time.Minute)
					health.NextCheckAt = &next
					if err = store.SaveKeyHealth(ctx, key, health); err != nil {
						return err
					}
				}
				pending = append(pending, pendingNotification{key: key, health: health})
			}
		}
	}
	for i := range pending {
		if ctx.Err() != nil {
			break
		}
		s.notifyKeyHealth(ctx, store, site, pending[i].key, &pending[i].health)
		if inventoryErr != nil {
			retry := now.Add(5 * time.Minute)
			pending[i].health.NextCheckAt = &retry
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			persistErr := store.SaveKeyHealth(persistCtx, pending[i].key, pending[i].health)
			cancel()
			if persistErr != nil {
				return persistErr
			}
		}
	}
	return nil
}

// A key-health pause owns only a matching governance account. Manual pauses,
// identity changes, and pauses for a missing group remain untouched.
func (s *Service) protectKeyAccount(ctx context.Context, site Site, key ManagedKey, health KeyHealth) error {
	local, ok := s.local.(ReconciliationLocal)
	if !ok {
		return nil
	}
	bindings, err := s.store.ListBindings(ctx, site.ID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.Marker != key.Marker || binding.AccountID <= 0 || binding.KeyCipher == "" {
			continue
		}
		if binding.SiteID != site.ID || binding.RemoteGroupID != key.RemoteGroupID || binding.Platform != key.Platform {
			return ErrConflict
		}
		boundKey, err := s.decryptManagedKey(binding.KeyCipher)
		if err != nil || boundKey.ID != key.RemoteKeyID {
			return ErrConflict
		}
		managedKey, err := s.decryptManagedKey(key.KeyCipher)
		if err != nil || managedKey != boundKey {
			return ErrConflict
		}
		account, err := s.inspectReconciliationAccount(ctx, local, site, binding)
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
			return nil
		}
		if err != nil || account == nil {
			return err
		}
		confirmedMissing := health.Status == KeyHealthConfirmedMissing || health.Status == KeyHealthGroupChanged && health.MissingCount >= 2
		if confirmedMissing && account.Status == "active" && account.Schedulable && account.PauseToken == "" {
			patch := ManagedAccountPatch{BindingID: binding.ID, Marker: binding.Marker, OperationID: uuid.NewString(), Expected: *account, Availability: "pause", PauseReason: "upstream_key_missing"}
			if _, err = local.ApplyManagedPatch(ctx, patch); err != nil {
				if errors.Is(err, ErrConflict) {
					return nil
				}
				return err
			}
			return s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "key_account_paused", Resource: key.Marker, After: "upstream_key_missing", CreatedAt: s.now()})
		}
		if health.Status != KeyHealthPresent || account.PauseReason != "upstream_key_missing" || account.PauseMarker != binding.Marker || account.PauseIdentity != account.Identity || account.Schedulable || !account.CanRestore {
			return nil
		}
		snapshot, err := s.store.LatestSnapshot(ctx, site.ID)
		if err != nil || snapshot == nil || !reconciliationSnapshotFresh(site, *snapshot, s.now()) {
			return nil
		}
		groupPresent := false
		for _, group := range snapshot.Catalog.Groups {
			if group.ID == key.RemoteGroupID {
				groupPresent = true
				break
			}
		}
		if !groupPresent {
			return nil
		}
		patch := ManagedAccountPatch{BindingID: binding.ID, Marker: binding.Marker, OperationID: uuid.NewString(), Expected: *account, Availability: "restore"}
		if _, err = local.ApplyManagedPatch(ctx, patch); err != nil {
			if errors.Is(err, ErrConflict) {
				return nil
			}
			return err
		}
		return s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "key_account_restored", Resource: key.Marker, After: "healthy", CreatedAt: s.now()})
	}
	return nil
}
