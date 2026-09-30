package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	KeyRepairPrepared           = "prepared"
	KeyRepairPostIntent         = "post_intent"
	KeyRepairAwaitingVisibility = "awaiting_visibility"
	KeyRepairCandidateReady     = "candidate_ready"
	KeyRepairCommitted          = "committed"
	KeyRepairConflict           = "conflict"
	KeyRepairAbandoned          = "abandoned"
)

// KeyRepair is a durable, reviewed remote creation operation. Private fields
// are retained for idempotent recovery and CAS, never returned to the client.
type KeyRepair struct {
	ID                         string           `json:"id"`
	SiteID                     int64            `json:"site_id"`
	ManagedKeyID               int64            `json:"managed_key_id"`
	AccountID                  int64            `json:"account_id"`
	AccountName                string           `json:"account_name"`
	RemoteGroupID              string           `json:"remote_group_id"`
	Platform                   string           `json:"platform"`
	OldRemoteKeyID             string           `json:"old_remote_key_id"`
	PlannedKeyName             string           `json:"planned_key_name"`
	CandidateRemoteKeyID       string           `json:"candidate_remote_key_id,omitempty"`
	Stage                      string           `json:"stage"`
	CanReprepare               bool             `json:"can_reprepare"`
	CanAbandon                 bool             `json:"can_abandon"`
	ErrorCode                  string           `json:"error_code,omitempty"`
	CreatedAt                  time.Time        `json:"created_at"`
	UpdatedAt                  time.Time        `json:"updated_at"`
	BindingID                  int64            `json:"-"`
	SiteVersion                int64            `json:"-"`
	OwnerUserID                int64            `json:"-"`
	Marker                     string           `json:"-"`
	BaseURL                    string           `json:"-"`
	OldKeyCipher               string           `json:"-"`
	OldCreationPlan            *KeyCreationPlan `json:"-"`
	ExpectedAccountFingerprint string           `json:"-"`
	ExpectedAccountIdentity    string           `json:"-"`
	Plan                       KeyCreationPlan  `json:"-"`
	IdempotencyKey             string           `json:"-"`
	CandidateKeyCipher         string           `json:"-"`
	PostIntentAt               *time.Time       `json:"-"`
}

type PrepareKeyRepairInput struct {
	SiteVersion int64 `json:"site_version"`
}

type AbandonKeyRepairInput struct {
	AcknowledgeUncertainCreate bool `json:"acknowledge_uncertain_create"`
}

type KeyRepairCommitRequest struct {
	Repair              KeyRepair
	OldKey              RemoteKey
	CandidateKey        RemoteKey
	CandidateVerifiedAt time.Time
}

type KeyRepairCommitter interface {
	CommitKeyRepair(context.Context, KeyRepairCommitRequest) error
}

type KeyRepairStore interface {
	ReserveKeyRepair(context.Context, *KeyRepair) error
	GetKeyRepair(context.Context, int64, int64, string) (*KeyRepair, error)
	LatestKeyRepair(context.Context, int64, int64) (*KeyRepair, error)
	SaveKeyRepairProgress(context.Context, *KeyRepair, string) error
}

var errRepairCatalogStale = errors.New("repair catalog is stale")

func (s *Service) LatestKeyRepair(ctx context.Context, siteID, keyID int64) (*KeyRepair, error) {
	if siteID <= 0 || keyID <= 0 {
		return nil, ErrInvalid
	}
	if _, err := s.store.GetSite(ctx, siteID); err != nil {
		return nil, err
	}
	store, ok := s.store.(KeyRepairStore)
	if !ok {
		return nil, ErrUnsupported
	}
	repair, err := store.LatestKeyRepair(ctx, siteID, keyID)
	return repairCapabilities(repair), err
}

func (s *Service) PrepareKeyRepair(ctx context.Context, siteID, keyID int64, input PrepareKeyRepairInput) (*KeyRepair, error) {
	if siteID <= 0 || keyID <= 0 || input.SiteVersion <= 0 {
		return nil, ErrInvalid
	}
	store, ok := s.store.(KeyRepairStore)
	if !ok {
		return nil, ErrUnsupported
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
	if !site.Enabled || site.Version != input.SiteVersion {
		return nil, ErrConflict
	}
	key, err := s.store.GetManagedKey(ctx, siteID, keyID)
	if err != nil {
		return nil, err
	}
	if key.KeyCipher == "" || key.RemoteKeyID == "" || key.Health.Status != KeyHealthConfirmedMissing || key.Health.MissingCount < 2 {
		return nil, ErrConflict
	}
	session, err := s.managementSessionLocked(ctx, site, false)
	if err != nil {
		return nil, err
	}
	if session.UserID <= 0 || session.UserID != key.OwnerUserID {
		return nil, ErrConflict
	}
	group, err := s.repairGroup(ctx, *site, key.RemoteGroupID, key.Platform)
	if err != nil {
		if errors.Is(err, errRepairCatalogStale) {
			return nil, ErrConflict
		}
		return nil, err
	}
	binding, account, managedAccount, err := s.repairAccount(ctx, *site, *key)
	if err != nil {
		return nil, err
	}
	inventory, err := s.inventoryForSite(ctx, *site, session, []ManagedKey{*key})
	if err != nil {
		if errors.Is(err, ErrReauth) {
			return nil, ErrReauth
		}
		return nil, ErrUpstreamKeyUnverifiable
	}
	if _, exists := inventory[key.RemoteKeyID]; exists {
		return nil, ErrConflict
	}
	id := uuid.NewString()
	name := repairKeyName(group, s.now(), site.Platform, id)
	existing, err := s.connector.PrepareKey(ctx, *site, session, group, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		existing = []int64{}
	}
	now := s.now().UTC()
	repair := &KeyRepair{ID: id, SiteID: site.ID, ManagedKeyID: key.ID, BindingID: binding.ID, AccountID: binding.AccountID, AccountName: account.Name, SiteVersion: site.Version, OwnerUserID: session.UserID, Marker: key.Marker, RemoteGroupID: key.RemoteGroupID, Platform: key.Platform, BaseURL: site.BaseURL, OldRemoteKeyID: key.RemoteKeyID, PlannedKeyName: name, OldKeyCipher: key.KeyCipher, OldCreationPlan: key.CreationPlan, ExpectedAccountFingerprint: account.Fingerprint, ExpectedAccountIdentity: managedAccount.Identity, Plan: KeyCreationPlan{Name: name, ExistingIDs: append([]int64{}, existing...)}, IdempotencyKey: repairIdempotencyKey(id), Stage: KeyRepairPrepared, CreatedAt: now, UpdatedAt: now}
	if err = store.ReserveKeyRepair(ctx, repair); err != nil {
		return nil, err
	}
	return repairCapabilities(repair), nil
}

func (s *Service) ConfirmKeyRepair(ctx context.Context, siteID, keyID int64, repairID string) (*KeyRepair, error) {
	if siteID <= 0 || keyID <= 0 || repairID == "" {
		return nil, ErrInvalid
	}
	store, ok := s.store.(KeyRepairStore)
	if !ok {
		return nil, ErrUnsupported
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
	repair, err := store.GetKeyRepair(ctx, siteID, keyID, repairID)
	if err != nil {
		return nil, err
	}
	repairCapabilities(repair)
	if repair.Stage == KeyRepairCommitted {
		if err := s.finishCommittedKeyRepair(ctx, *site, *repair); err != nil {
			return repairCapabilities(repair), err
		}
		return repairCapabilities(repair), nil
	}
	if repair.Stage == KeyRepairConflict || repair.Stage == KeyRepairAbandoned {
		return repairCapabilities(repair), nil
	}
	group, session, err := s.validateRepairContext(ctx, *site, *repair)
	if errors.Is(err, errRepairCatalogStale) {
		return repair, ErrConflict
	}
	if errors.Is(err, ErrConflict) {
		return s.conflictKeyRepair(ctx, store, repair)
	}
	if err != nil {
		return nil, err
	}
	recovery, ok := s.connector.(KeyRepairRecovery)
	if !ok {
		return nil, ErrUnsupported
	}
	creator, ok := s.connector.(KeyRepairCreateOnce)
	if !ok {
		return nil, ErrUnsupported
	}
	oldInventory, inventoryErr := s.inventoryForSite(ctx, *site, session, []ManagedKey{{SiteID: repair.SiteID, OwnerUserID: repair.OwnerUserID, KeyCipher: repair.OldKeyCipher}})
	if inventoryErr != nil {
		if errors.Is(inventoryErr, ErrReauth) {
			return repair, ErrReauth
		}
		return repair, ErrUpstreamKeyUnverifiable
	}
	if _, returned := oldInventory[repair.OldRemoteKeyID]; returned {
		return s.conflictKeyRepair(ctx, store, repair)
	}
	var candidate RemoteKey
	var verifiedAt time.Time
	switch repair.Stage {
	case KeyRepairPrepared:
		// A candidate appearing before our POST cannot be attributed to this operation.
		if _, found, readErr := recovery.RecoverPlannedKey(ctx, *site, session, group, repair.Plan); readErr != nil {
			return nil, readErr
		} else if found {
			return s.conflictKeyRepair(ctx, store, repair)
		}
		previous := repair.Stage
		now := s.now().UTC()
		repair.Stage, repair.PostIntentAt, repair.UpdatedAt = KeyRepairPostIntent, &now, now
		if err = store.SaveKeyRepairProgress(ctx, repair, previous); err != nil {
			return nil, err
		}
		if err = creator.PostPlannedKey(ctx, *site, session, group, repair.IdempotencyKey, repair.Plan); err != nil {
			return s.awaitKeyRepair(ctx, store, repair, err)
		}
		var found bool
		candidate, found, err = recovery.RecoverPlannedKey(ctx, *site, session, group, repair.Plan)
		if err != nil || !found {
			return s.awaitKeyRepair(ctx, store, repair, err)
		}
		verifiedAt = s.now().UTC()
	case KeyRepairPostIntent, KeyRepairAwaitingVisibility:
		var found bool
		candidate, found, err = recovery.RecoverPlannedKey(ctx, *site, session, group, repair.Plan)
		if err != nil || !found {
			return s.awaitKeyRepair(ctx, store, repair, err)
		}
		verifiedAt = s.now().UTC()
	case KeyRepairCandidateReady:
		var found bool
		candidate, found, err = recovery.RecoverPlannedKey(ctx, *site, session, group, repair.Plan)
		if err != nil {
			return repair, ErrUpstreamKeyUnverifiable
		}
		if !found {
			repair.ErrorCode, repair.UpdatedAt = "upstream_key_unverifiable", s.now().UTC()
			if err = store.SaveKeyRepairProgress(ctx, repair, KeyRepairCandidateReady); err != nil {
				return nil, err
			}
			return repair, nil
		}
		if candidate.ID != repair.CandidateRemoteKeyID {
			return s.conflictKeyRepair(ctx, store, repair)
		}
		verifiedAt = s.now().UTC()
	default:
		return nil, ErrConflict
	}
	if candidate.ID == "" || candidate.ID == repair.OldRemoteKeyID || candidate.Key == "" {
		return s.conflictKeyRepair(ctx, store, repair)
	}
	if repair.Stage != KeyRepairCandidateReady {
		raw, marshalErr := json.Marshal(candidate)
		if marshalErr != nil {
			return nil, ErrInvalid
		}
		cipher, encryptErr := s.cipher.Encrypt(string(raw))
		if encryptErr != nil {
			return nil, ErrEncryption
		}
		previous := repair.Stage
		repair.Stage, repair.CandidateRemoteKeyID, repair.CandidateKeyCipher, repair.ErrorCode, repair.UpdatedAt = KeyRepairCandidateReady, candidate.ID, cipher, "", s.now().UTC()
		if err = store.SaveKeyRepairProgress(ctx, repair, previous); err != nil {
			return nil, err
		}
	} else {
		stored, decryptErr := s.decryptManagedKey(repair.CandidateKeyCipher)
		if decryptErr != nil || stored != candidate {
			return s.conflictKeyRepair(ctx, store, repair)
		}
	}
	oldKey, err := s.decryptManagedKey(repair.OldKeyCipher)
	if err != nil || oldKey.ID != repair.OldRemoteKeyID {
		return s.conflictKeyRepair(ctx, store, repair)
	}
	verifiedInventory, inventoryErr := s.inventoryForSite(ctx, *site, session, []ManagedKey{{SiteID: repair.SiteID, OwnerUserID: repair.OwnerUserID, KeyCipher: repair.OldKeyCipher}})
	_, oldReturned := verifiedInventory[repair.OldRemoteKeyID]
	if oldReturned {
		return s.conflictKeyRepair(ctx, store, repair)
	}
	if inventoryErr != nil || checkedKeyInventory(verifiedInventory, candidate.ID, repair.RemoteGroupID) != nil {
		repair.ErrorCode = "upstream_key_unverifiable"
		if inventoryErr != nil {
			repair.ErrorCode = ErrorCode(inventoryErr)
		}
		repair.UpdatedAt = s.now().UTC()
		if err = store.SaveKeyRepairProgress(ctx, repair, KeyRepairCandidateReady); err != nil {
			return nil, err
		}
		return repair, nil
	}
	verifiedAt = s.now().UTC()
	if repair.ErrorCode != "" {
		repair.ErrorCode, repair.UpdatedAt = "", verifiedAt
		if err = store.SaveKeyRepairProgress(ctx, repair, KeyRepairCandidateReady); err != nil {
			return nil, err
		}
	}
	committer, ok := s.local.(KeyRepairCommitter)
	if !ok {
		return nil, ErrUnsupported
	}
	if err = committer.CommitKeyRepair(ctx, KeyRepairCommitRequest{Repair: *repair, OldKey: oldKey, CandidateKey: candidate, CandidateVerifiedAt: verifiedAt}); err != nil {
		if errors.Is(err, ErrConflict) {
			return s.conflictKeyRepair(ctx, store, repair)
		}
		return repair, err
	}
	repair, err = store.GetKeyRepair(ctx, siteID, keyID, repairID)
	if err != nil {
		return nil, err
	}
	if repair.Stage != KeyRepairCommitted {
		return nil, ErrConflict
	}
	if err := s.finishCommittedKeyRepair(ctx, *site, *repair); err != nil {
		return repair, err
	}
	return repair, nil
}

// A committed remote/local switch cannot be rolled back because follow-up
// availability or mail delivery failed. Replays retry those guarded effects.
func (s *Service) finishCommittedKeyRepair(ctx context.Context, site Site, repair KeyRepair) error {
	if !site.Enabled || site.ID != repair.SiteID || site.BaseURL != repair.BaseURL {
		return nil
	}
	postCommitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	updated, err := s.store.GetManagedKey(postCommitCtx, site.ID, repair.ManagedKeyID)
	if err != nil {
		return err
	}
	if updated.OwnerUserID != repair.OwnerUserID || updated.Marker != repair.Marker || updated.RemoteGroupID != repair.RemoteGroupID || updated.Platform != repair.Platform || updated.RemoteKeyID != repair.CandidateRemoteKeyID || updated.KeyCipher != repair.CandidateKeyCipher || updated.Health.Status != KeyHealthPresent {
		return nil
	}
	protectionErr := s.protectKeyAccount(postCommitCtx, site, *updated, updated.Health)
	if healthStore, ok := s.store.(KeyHealthStore); ok {
		health := updated.Health
		s.notifyKeyHealth(postCommitCtx, healthStore, site, *updated, &health)
		if protectionErr != nil {
			health.ProtectionError = ErrorCode(protectionErr)
			next := s.now().UTC().Add(time.Minute)
			health.NextCheckAt = &next
		} else if health.ProtectionError != "" {
			health.ProtectionError = ""
			if health.NotificationKind == "" || health.NotificationStatus == "sent" {
				health.NextCheckAt = nil
			}
		} else {
			return nil
		}
		persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		err = healthStore.SaveKeyHealth(persistCtx, *updated, health)
		persistCancel()
		return err
	}
	return protectionErr
}

// AbandonKeyRepair is an explicit administrator acknowledgement, never an
// inference from a create error. A complete read-only inventory must still
// show neither the old key nor an operation-owned candidate.
func (s *Service) AbandonKeyRepair(ctx context.Context, siteID, keyID int64, repairID string, input AbandonKeyRepairInput) (*KeyRepair, error) {
	if siteID <= 0 || keyID <= 0 || repairID == "" || !input.AcknowledgeUncertainCreate {
		return nil, ErrInvalid
	}
	store, ok := s.store.(KeyRepairStore)
	if !ok {
		return nil, ErrUnsupported
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
	repair, err := store.GetKeyRepair(ctx, siteID, keyID, repairID)
	if err != nil {
		return nil, err
	}
	repairCapabilities(repair)
	if repair.Stage == KeyRepairAbandoned {
		return repairCapabilities(repair), nil
	}
	if !repairMayAbandon(*repair) {
		return nil, ErrConflict
	}
	session, err := s.validateAbandonContext(ctx, *site, *repair)
	if err != nil {
		return nil, err
	}
	inventory, err := s.inventoryForSite(ctx, *site, session, []ManagedKey{{SiteID: repair.SiteID, OwnerUserID: repair.OwnerUserID, KeyCipher: repair.OldKeyCipher}})
	if err != nil {
		return nil, ErrUpstreamKeyUnverifiable
	}
	if _, visible := inventory[repair.OldRemoteKeyID]; visible {
		return nil, ErrConflict
	}
	recovery, ok := s.connector.(KeyRepairRecovery)
	if !ok {
		return nil, ErrUnsupported
	}
	if _, found, err := recovery.RecoverPlannedKey(ctx, *site, session, RemoteGroup{ID: repair.RemoteGroupID}, repair.Plan); err != nil {
		return nil, ErrUpstreamKeyUnverifiable
	} else if found {
		return nil, ErrConflict
	}
	previous := repair.Stage
	repair.Stage, repair.ErrorCode, repair.UpdatedAt = KeyRepairAbandoned, "manually_abandoned", s.now().UTC()
	if err = store.SaveKeyRepairProgress(ctx, repair, previous); err != nil {
		return nil, err
	}
	_ = s.store.AddEvent(ctx, &Event{SiteID: siteID, Kind: "key_repair_abandoned", Resource: repair.Marker, After: repair.ID, CreatedAt: s.now().UTC()})
	return repairCapabilities(repair), nil
}

func (s *Service) validateAbandonContext(ctx context.Context, site Site, repair KeyRepair) (Session, error) {
	if site.ID != repair.SiteID || site.BaseURL != repair.BaseURL {
		return Session{}, ErrConflict
	}
	key, err := s.store.GetManagedKey(ctx, site.ID, repair.ManagedKeyID)
	if err != nil {
		return Session{}, err
	}
	if key.OwnerUserID != repair.OwnerUserID || key.Marker != repair.Marker || key.RemoteGroupID != repair.RemoteGroupID || key.Platform != repair.Platform || key.RemoteKeyID != repair.OldRemoteKeyID || key.KeyCipher != repair.OldKeyCipher {
		return Session{}, ErrConflict
	}
	session, err := s.managementSessionLocked(ctx, &site, false)
	if err != nil {
		return Session{}, err
	}
	if session.UserID <= 0 || session.UserID != repair.OwnerUserID {
		return Session{}, ErrConflict
	}
	return session, nil
}

func (s *Service) repairGroup(ctx context.Context, site Site, groupID, platform string) (RemoteGroup, error) {
	snapshot, err := s.store.LatestSnapshot(ctx, site.ID)
	if err != nil {
		return RemoteGroup{}, err
	}
	if !reconciliationSnapshotFresh(site, *snapshot, s.now()) {
		return RemoteGroup{}, errRepairCatalogStale
	}
	for _, group := range snapshot.Catalog.Groups {
		if group.ID == groupID && validSiteTransport(site.Platform, platform) && compatibleTransport(group.Platform, platform) {
			return group, nil
		}
	}
	return RemoteGroup{}, ErrConflict
}

func (s *Service) repairAccount(ctx context.Context, site Site, key ManagedKey) (Binding, *LocalAccount, *ManagedLocalAccount, error) {
	bindings, err := s.store.ListBindings(ctx, site.ID)
	if err != nil {
		return Binding{}, nil, nil, err
	}
	for _, binding := range bindings {
		if binding.Marker != key.Marker {
			continue
		}
		if binding.SiteID != site.ID || binding.RemoteGroupID != key.RemoteGroupID || binding.Platform != key.Platform || binding.AccountID <= 0 || binding.KeyCipher != key.KeyCipher {
			return Binding{}, nil, nil, ErrConflict
		}
		account, err := s.local.FindAccount(ctx, binding.Marker)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
				return Binding{}, nil, nil, ErrConflict
			}
			return Binding{}, nil, nil, err
		}
		if account == nil || account.ID != binding.AccountID || account.Fingerprint == "" {
			return Binding{}, nil, nil, ErrConflict
		}
		local, ok := s.local.(ReconciliationLocal)
		if !ok {
			return Binding{}, nil, nil, ErrUnsupported
		}
		managed, err := s.inspectReconciliationAccount(ctx, local, site, binding)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
				return Binding{}, nil, nil, ErrConflict
			}
			return Binding{}, nil, nil, err
		}
		if managed == nil || managed.Identity == "" || managed.Schedulable {
			return Binding{}, nil, nil, ErrConflict
		}
		return binding, account, managed, nil
	}
	return Binding{}, nil, nil, ErrConflict
}

func (s *Service) validateRepairContext(ctx context.Context, site Site, repair KeyRepair) (RemoteGroup, Session, error) {
	if !site.Enabled || site.ID != repair.SiteID || site.Version != repair.SiteVersion || site.BaseURL != repair.BaseURL {
		return RemoteGroup{}, Session{}, ErrConflict
	}
	key, err := s.store.GetManagedKey(ctx, site.ID, repair.ManagedKeyID)
	if err != nil {
		return RemoteGroup{}, Session{}, err
	}
	if key.OwnerUserID != repair.OwnerUserID || key.Marker != repair.Marker || key.RemoteGroupID != repair.RemoteGroupID || key.Platform != repair.Platform || key.RemoteKeyID != repair.OldRemoteKeyID || key.KeyCipher != repair.OldKeyCipher || key.Health.Status != KeyHealthConfirmedMissing || key.Health.MissingCount < 2 {
		return RemoteGroup{}, Session{}, ErrConflict
	}
	group, err := s.repairGroup(ctx, site, repair.RemoteGroupID, repair.Platform)
	if err != nil {
		return RemoteGroup{}, Session{}, err
	}
	binding, account, managed, err := s.repairAccount(ctx, site, *key)
	if err != nil {
		return RemoteGroup{}, Session{}, err
	}
	if binding.ID != repair.BindingID || binding.AccountID != repair.AccountID || account.Fingerprint != repair.ExpectedAccountFingerprint || managed.Identity != repair.ExpectedAccountIdentity {
		return RemoteGroup{}, Session{}, ErrConflict
	}
	session, err := s.managementSessionLocked(ctx, &site, false)
	if err != nil {
		return RemoteGroup{}, Session{}, err
	}
	if session.UserID <= 0 || session.UserID != repair.OwnerUserID {
		return RemoteGroup{}, Session{}, ErrConflict
	}
	return group, session, nil
}

func (s *Service) awaitKeyRepair(ctx context.Context, store KeyRepairStore, repair *KeyRepair, cause error) (*KeyRepair, error) {
	previous := repair.Stage
	repair.Stage = KeyRepairAwaitingVisibility
	repair.ErrorCode = "candidate_not_visible"
	if cause != nil {
		repair.ErrorCode = ErrorCode(cause)
	}
	repair.UpdatedAt = s.now().UTC()
	if err := store.SaveKeyRepairProgress(ctx, repair, previous); err != nil {
		return nil, err
	}
	return repair, nil
}

func (s *Service) conflictKeyRepair(ctx context.Context, store KeyRepairStore, repair *KeyRepair) (*KeyRepair, error) {
	previous := repair.Stage
	repair.Stage, repair.ErrorCode, repair.UpdatedAt = KeyRepairConflict, "stale_preview", s.now().UTC()
	if err := store.SaveKeyRepairProgress(ctx, repair, previous); err != nil {
		return nil, err
	}
	return repairCapabilities(repair), nil
}

func repairCapabilities(repair *KeyRepair) *KeyRepair {
	if repair != nil {
		repair.PlannedKeyName = repair.Plan.Name
		repair.CanReprepare = repair.Stage == KeyRepairAbandoned || repair.Stage == KeyRepairConflict && repair.PostIntentAt == nil
		repair.CanAbandon = repairMayAbandon(*repair)
	}
	return repair
}

func repairMayAbandon(repair KeyRepair) bool {
	return repair.PostIntentAt != nil && repair.CandidateKeyCipher == "" &&
		(repair.Stage == KeyRepairPostIntent || repair.Stage == KeyRepairAwaitingVisibility || repair.Stage == KeyRepairConflict)
}

func repairKeyName(group RemoteGroup, now time.Time, platform, id string) string {
	base := managedKeyName(group, now, platform)
	suffix := "-r" + strings.ReplaceAll(id, "-", "")[:8]
	maxRunes := 100
	if platform == "newapi" {
		maxRunes = 30
	}
	for len(base)+len(suffix) > 100 || utf8.RuneCountInString(base)+utf8.RuneCountInString(suffix) > maxRunes {
		_, size := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-size]
	}
	return strings.TrimSpace(base) + suffix
}

func repairIdempotencyKey(id string) string {
	return "sub2api-governance-repair-" + strings.ReplaceAll(id, "-", "")[:24]
}
