package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (m *memoryStore) ListManagedKeys(_ context.Context, siteID int64) ([]ManagedKey, error) {
	keys := []ManagedKey{}
	for _, key := range m.keys {
		if key.SiteID == siteID {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (m *memoryStore) GetManagedKey(_ context.Context, siteID, id int64) (*ManagedKey, error) {
	for _, key := range m.keys {
		if key.SiteID == siteID && key.ID == id {
			return &key, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memoryStore) SaveManagedKey(_ context.Context, key *ManagedKey) error {
	key.HasKey = key.KeyCipher != ""
	key.UpdatedAt = time.Now().UTC()
	for i, previous := range m.keys {
		if previous.SiteID == key.SiteID && previous.RemoteGroupID == key.RemoteGroupID && previous.Platform == key.Platform {
			if previous.OwnerUserID != key.OwnerUserID || previous.Marker != key.Marker || previous.KeyCipher != "" && (previous.KeyCipher != key.KeyCipher || previous.RemoteKeyID != key.RemoteKeyID) {
				return ErrConflict
			}
			key.ID = previous.ID
			key.CreatedAt = previous.CreatedAt
			m.keys[i] = *key
			return nil
		}
	}
	key.ID = int64(len(m.keys) + 1)
	key.CreatedAt = key.UpdatedAt
	m.keys = append(m.keys, *key)
	return nil
}

func TestManagedKeysCreateIndependentlyAndImportReusesThem(t *testing.T) {
	s, m, c, local := setupEngine(t)
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	request := CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}}
	result, err := s.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "created", result.Items[0].Status)
	require.Equal(t, "fixture-inference-key", result.Items[0].Key)
	require.NotNil(t, result.Items[0].ManagedKey)
	require.Empty(t, m.bindings, "key creation must not create an import binding")
	require.Zero(t, local.calls, "key creation must not create a local account")
	require.Len(t, m.keys, 1)
	require.Equal(t, int64(5), m.keys[0].OwnerUserID)
	require.NotContains(t, m.keys[0].KeyCipher, "fixture-inference-key")

	again, err := s.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Equal(t, "reused", again.Items[0].Status)
	require.Equal(t, result.Items[0].ManagedKey.ID, again.Items[0].ManagedKey.ID)
	require.Equal(t, 1, c.keyCalls, "repeating creation must reuse the managed key")
	metadata, err := s.Keys(t.Context(), 1)
	require.NoError(t, err)
	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fixture-inference-key")
	require.NotContains(t, string(raw), m.keys[0].KeyCipher)
	require.NotContains(t, string(raw), "owner_user_id")
	require.True(t, metadata[0].HasKey)
	revealed, err := s.RevealKey(t.Context(), 1, metadata[0].ID)
	require.NoError(t, err)
	require.Equal(t, "fixture-inference-key", revealed.Key)
	_, err = s.RevealKey(t.Context(), 2, metadata[0].ID)
	require.ErrorIs(t, err, ErrNotFound)

	preview, err := s.Preview(t.Context(), 1, selections())
	require.NoError(t, err)
	require.False(t, preview.Rows[0].WillCreateKey)
	applied, err := s.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", applied.Items[0].Status)
	require.Equal(t, 1, c.keyCalls)
	require.Equal(t, "fixture-inference-key", local.changes[0].APIKey)
}

func TestManagedKeysAdoptExistingImportBindingWithoutRemoteCreation(t *testing.T) {
	s, m, c, local := setupEngine(t)
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	raw, err := json.Marshal(RemoteKey{ID: "legacy-remote", Key: "legacy-key-canary"})
	require.NoError(t, err)
	encrypted, err := s.cipher.Encrypt(string(raw))
	require.NoError(t, err)
	m.bindings = []Binding{{ID: 1, SiteID: 1, RemoteGroupID: "8", Platform: "openai", Marker: marker(1, "8", "openai"), LocalGroupID: 7, AccountID: 10, KeyCipher: encrypted}}
	result, err := s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
	require.NoError(t, err)
	require.Equal(t, "reused", result.Items[0].Status)
	require.Equal(t, "legacy-key-canary", result.Items[0].Key)
	require.Equal(t, "legacy-remote", result.Items[0].ManagedKey.RemoteKeyID)
	require.Zero(t, c.keyCalls)
	require.Zero(t, local.calls)
}

func TestManagedKeysRejectStaleAndInvisibleSelectionsBeforeRemoteEffects(t *testing.T) {
	for _, invalid := range []string{"snapshot", "site-version", "hidden-group", "transport", "duplicate", "too-many"} {
		t.Run(invalid, func(t *testing.T) {
			s, m, connector, local := setupEngine(t)
			snapshot, err := s.Sync(t.Context(), 1)
			require.NoError(t, err)
			request := CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}}
			want := ErrInvalid
			switch invalid {
			case "snapshot":
				request.SnapshotID++
				want = ErrConflict
			case "site-version":
				m.site.Version++
				want = ErrConflict
			case "hidden-group":
				request.Selections[0].RemoteGroupID = "private"
			case "transport":
				request.Selections[0].Platform = "anthropic"
			case "duplicate":
				request.Selections = append(request.Selections, request.Selections[0])
			case "too-many":
				request.Selections = make([]KeySelection, 101)
			}
			_, err = s.CreateKeys(t.Context(), 1, request)
			require.ErrorIs(t, err, want)
			require.Zero(t, connector.keyCalls)
			require.Empty(t, m.keys)
			require.Zero(t, local.calls)
		})
	}
}

type managedKeyConnector struct {
	*fakeConnector
	failGroup string
	loginUser int64
}

func (c *managedKeyConnector) EnsureKey(ctx context.Context, site Site, session Session, group RemoteGroup, marker string) (RemoteKey, error) {
	if group.ID == c.failGroup {
		c.keyCalls++
		return RemoteKey{}, errors.New("upstream body with secret-canary")
	}
	return c.fakeConnector.EnsureKey(ctx, site, session, group, marker)
}

func (c *managedKeyConnector) Login(context.Context, Site, LoginInput) (Session, *Challenge, error) {
	return Session{AccessToken: "fixture-session", UserID: c.loginUser}, nil, nil
}

func TestManagedKeysPartialBatchCanRetryWithoutDuplicatingSuccesses(t *testing.T) {
	s, m, connector, local := setupEngine(t)
	connector.catalog.Groups = append(connector.catalog.Groups, RemoteGroup{ID: "9", Name: "Second", Platform: "openai"})
	s.connector = &managedKeyConnector{fakeConnector: connector, failGroup: "9", loginUser: 5}
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	request := CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}, {RemoteGroupID: "9", Platform: "openai"}}}
	result, err := s.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Equal(t, "created", result.Items[0].Status)
	require.Equal(t, "failed", result.Items[1].Status)
	require.Equal(t, "operation_failed", result.Items[1].Error)
	require.Empty(t, result.Items[1].Key)
	require.Len(t, m.keys, 2, "pending remote effects must preserve their owner")
	require.False(t, m.keys[1].HasKey)
	_, err = s.RevealKey(t.Context(), 1, m.keys[1].ID)
	require.ErrorIs(t, err, ErrNotFound)
	s.connector.(*managedKeyConnector).failGroup = ""
	result, err = s.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Equal(t, "reused", result.Items[0].Status)
	require.Equal(t, "created", result.Items[1].Status)
	require.Equal(t, 3, connector.keyCalls)
	require.Zero(t, local.calls)
	require.Empty(t, m.bindings)
}

func TestManagedKeysProtectKeyOnlySiteOwnership(t *testing.T) {
	s, m, connector, _ := setupEngine(t)
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	_, err = s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
	require.NoError(t, err)
	input := m.site
	input.BaseURL = "https://other.example"
	_, err = s.UpdateSite(t.Context(), 1, input)
	require.ErrorIs(t, err, ErrConflict)
	s.connector = &managedKeyConnector{fakeConnector: connector, loginUser: 6}
	_, err = s.Connect(t.Context(), 1, LoginInput{Username: "other", Password: "transient"})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, "https://upstream.example", m.site.BaseURL)
	require.Equal(t, int64(1), m.site.Version)
	// The durable owner also lets the same owner repair an unreadable old session.
	m.site.SessionCipher = "unreadable-session"
	s.connector.(*managedKeyConnector).loginUser = 5
	_, err = s.Connect(t.Context(), 1, LoginInput{Username: "same", Password: "transient"})
	require.NoError(t, err)
}

func TestBulkPreviewAcceptsHundredGroupsAndGrokOpenAITransport(t *testing.T) {
	s, _, connector, local := setupEngine(t)
	connector.catalog.Groups = nil
	selected := []Selection{}
	keys := []KeySelection{}
	for i := 0; i < 100; i++ {
		id := fmt.Sprint(i + 1)
		connector.catalog.Groups = append(connector.catalog.Groups, RemoteGroup{ID: id, Name: "Grok", Platform: "grok"})
		selected = append(selected, Selection{RemoteGroupID: id, Platform: "openai", LocalGroupID: 7, AccountName: "Grok import", CostMultiplier: 1})
		keys = append(keys, KeySelection{RemoteGroupID: id, Platform: "openai"})
	}
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	preview, err := s.Preview(t.Context(), 1, selected)
	require.NoError(t, err)
	require.Len(t, preview.Rows, 100)
	result, err := s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: keys})
	require.NoError(t, err)
	require.Len(t, result.Items, 100)
	require.Equal(t, "created", result.Items[99].Status)
	require.Zero(t, local.calls)
}

func TestUnknownGroupsRequireExplicitTransport(t *testing.T) {
	for _, platform := range []string{"", "unknown", "composite", "custom"} {
		t.Run(platform, func(t *testing.T) {
			s, _, connector, _ := setupEngine(t)
			connector.catalog.Groups[0].Platform = platform
			snapshot, err := s.Sync(t.Context(), 1)
			require.NoError(t, err)
			_, err = s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8"}}})
			require.ErrorIs(t, err, ErrInvalid)
			_, err = s.Preview(t.Context(), 1, selections())
			if platform == "custom" {
				require.ErrorIs(t, err, ErrInvalid)
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, connector.keyCalls)
		})
	}
}

type failingManagedKeyStore struct {
	*memoryStore
}

func (s *failingManagedKeyStore) SaveManagedKey(context.Context, *ManagedKey) error {
	return errors.New("fixture storage failure")
}

func TestManagedKeysDoNotCreateRemotelyWithoutDurableOwnership(t *testing.T) {
	s, memory, connector, _ := setupEngine(t)
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	s.store = &failingManagedKeyStore{memoryStore: memory}
	result, err := s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
	require.NoError(t, err)
	require.Equal(t, "failed", result.Items[0].Status)
	require.Equal(t, "operation_failed", result.Items[0].Error)
	require.Zero(t, connector.keyCalls)
	require.Empty(t, memory.keys)
}

func TestManagedKeysRequireDurableEncryptionAndSiteLock(t *testing.T) {
	for _, state := range []string{"encryption", "busy", "unknown-owner"} {
		t.Run(state, func(t *testing.T) {
			s, memory, connector, _ := setupEngine(t)
			snapshot, err := s.Sync(t.Context(), 1)
			require.NoError(t, err)
			want := ErrEncryption
			switch state {
			case "encryption":
				s.durableKey = false
			case "busy":
				memory.locked = true
				want = ErrBusy
			case "unknown-owner":
				memory.site.SessionCipher, err = s.cipher.Encrypt(`{"access_token":"fixture-session"}`)
				require.NoError(t, err)
				want = ErrReauth
			}
			_, err = s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
			require.ErrorIs(t, err, want)
			require.Zero(t, connector.keyCalls)
			require.Empty(t, memory.keys)
		})
	}
}
