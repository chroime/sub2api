package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type plannedKeyConnector struct {
	*fakeConnector
	prepare func(string) ([]int64, error)
	ensure  func(string, *KeyCreationPlan) (RemoteKey, error)
}

func (c *plannedKeyConnector) PrepareKey(_ context.Context, _ Site, _ Session, _ RemoteGroup, name string) ([]int64, error) {
	c.prepareCalls++
	if c.prepare != nil {
		return c.prepare(name)
	}
	return []int64{}, nil
}

func (c *plannedKeyConnector) EnsureKey(_ context.Context, _ Site, _ Session, _ RemoteGroup, marker string, plan *KeyCreationPlan) (RemoteKey, error) {
	c.keyCalls++
	if c.ensure != nil {
		return c.ensure(marker, plan)
	}
	return RemoteKey{ID: "10", Key: "fixture-planned-key"}, nil
}

func TestManagedKeyCreationFreezesNameAndInventoryAcrossDaysAndRename(t *testing.T) {
	s, memory, fake, _ := setupEngine(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("Fixture/Shanghai", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })
	now := time.Date(2026, 9, 26, 16, 30, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	fake.catalog.Groups[0].Name = "优选分组"
	connector := &plannedKeyConnector{fakeConnector: fake}
	connector.prepare = func(name string) ([]int64, error) {
		require.Len(t, memory.keys, 1, "reserve ownership and name before listing remote keys")
		require.Equal(t, "优选分组-0.8-20260927", name)
		require.Equal(t, name, memory.keys[0].CreationPlan.Name)
		require.Nil(t, memory.keys[0].CreationPlan.ExistingIDs)
		return []int64{3, 7}, nil
	}
	connector.ensure = func(stable string, plan *KeyCreationPlan) (RemoteKey, error) {
		require.Equal(t, marker(1, "8", "openai"), stable)
		require.Equal(t, &KeyCreationPlan{Name: "优选分组-0.8-20260927", ExistingIDs: []int64{3, 7}}, plan)
		require.Equal(t, plan, memory.keys[0].CreationPlan, "persist inventory before any creation")
		if connector.keyCalls == 1 {
			return RemoteKey{}, errConnectorUncertain
		}
		return RemoteKey{ID: "10", Key: "fixture-planned-key"}, nil
	}
	s.connector = connector
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	request := CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}}
	first, err := s.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Equal(t, "failed", first.Items[0].Status)
	now = now.Add(48 * time.Hour)
	fake.catalog.Groups[0].Name = "上游已改名"
	newRate := 1.25
	fake.catalog.Groups[0].ResolvedRateMultiplier = &newRate
	snapshot, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	request.SnapshotID = snapshot.ID
	restarted := NewService(memory, connector, s.local, s.cipher, true)
	restarted.now = s.now
	second, err := restarted.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Equal(t, "created", second.Items[0].Status)
	require.Equal(t, 1, connector.prepareCalls, "retries retain the original inventory")
	require.Equal(t, 2, connector.keyCalls)
	third, err := restarted.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Equal(t, "reused", third.Items[0].Status)
	require.Equal(t, 2, connector.keyCalls)
}

type failPreparedKeyStore struct {
	*memoryStore
	fail bool
}

func (s *failPreparedKeyStore) SaveManagedKey(ctx context.Context, key *ManagedKey) error {
	if s.fail && key.CreationPlan != nil && key.CreationPlan.ExistingIDs != nil {
		return errors.New("fixture inventory persistence failure")
	}
	return s.memoryStore.SaveManagedKey(ctx, key)
}

func TestManagedKeyCreationNeverPostsBeforeInventoryIsDurable(t *testing.T) {
	s, memory, fake, _ := setupEngine(t)
	connector := &plannedKeyConnector{fakeConnector: fake, prepare: func(string) ([]int64, error) { return nil, nil }}
	s.connector = connector
	store := &failPreparedKeyStore{memoryStore: memory, fail: true}
	s.store = store
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	request := CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}}
	first, err := s.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Equal(t, "failed", first.Items[0].Status)
	require.Zero(t, connector.keyCalls)
	require.Len(t, memory.keys, 1)
	require.NotNil(t, memory.keys[0].CreationPlan)
	require.Nil(t, memory.keys[0].CreationPlan.ExistingIDs)
	store.fail = false
	second, err := s.CreateKeys(t.Context(), 1, request)
	require.NoError(t, err)
	require.Equal(t, "created", second.Items[0].Status)
	require.Equal(t, 2, connector.prepareCalls)
	require.Equal(t, 1, connector.keyCalls)
	require.NotNil(t, memory.keys[0].CreationPlan.ExistingIDs, "an empty completed inventory must remain distinguishable from no inventory")
	require.Empty(t, memory.keys[0].CreationPlan.ExistingIDs)
}

func TestManagedKeyCreationLegacyPendingRetainsMarkerName(t *testing.T) {
	s, memory, fake, _ := setupEngine(t)
	stable := marker(1, "8", "openai")
	memory.keys = []ManagedKey{{ID: 1, SiteID: 1, RemoteGroupID: "8", Platform: "openai", Marker: stable, OwnerUserID: 5}}
	connector := &plannedKeyConnector{fakeConnector: fake, ensure: func(receivedMarker string, plan *KeyCreationPlan) (RemoteKey, error) {
		require.Equal(t, stable, receivedMarker)
		require.Nil(t, plan)
		return RemoteKey{ID: "10", Key: "legacy-fixture-key"}, nil
	}}
	s.connector = connector
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	result, err := s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
	require.NoError(t, err)
	require.Equal(t, "created", result.Items[0].Status)
	require.Zero(t, connector.prepareCalls)
	require.Nil(t, memory.keys[0].CreationPlan)
}

func TestManagedKeyCreationRetryExcludesOtherCompletedProtocolWithoutChangingPlan(t *testing.T) {
	s, memory, fake, _ := setupEngine(t)
	fake.catalog.Groups[0].Platform = "unknown"
	stable := marker(1, "8", "openai")
	original := &KeyCreationPlan{Name: "Remote-20260927", ExistingIDs: []int64{1}}
	memory.keys = []ManagedKey{
		{ID: 1, SiteID: 1, RemoteGroupID: "8", Platform: "openai", Marker: stable, OwnerUserID: 5, CreationPlan: original},
		{ID: 2, SiteID: 1, RemoteGroupID: "8", Platform: "anthropic", Marker: marker(1, "8", "anthropic"), OwnerUserID: 5, RemoteKeyID: "4", KeyCipher: "fixture-cipher"},
		{ID: 3, SiteID: 1, RemoteGroupID: "9", Platform: "openai", Marker: marker(1, "9", "openai"), OwnerUserID: 5, RemoteKeyID: "5", KeyCipher: "fixture-cipher"},
	}
	connector := &plannedKeyConnector{fakeConnector: fake, ensure: func(receivedMarker string, plan *KeyCreationPlan) (RemoteKey, error) {
		require.Equal(t, stable, receivedMarker)
		require.Equal(t, &KeyCreationPlan{Name: original.Name, ExistingIDs: []int64{1, 4}}, plan)
		require.Equal(t, []int64{1}, memory.keys[0].CreationPlan.ExistingIDs)
		return RemoteKey{ID: "3", Key: "fixture-recovered-key"}, nil
	}}
	s.connector = connector
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	result, err := s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
	require.NoError(t, err)
	require.Equal(t, "created", result.Items[0].Status)
	require.Zero(t, connector.prepareCalls)
	require.Equal(t, []int64{1}, memory.keys[0].CreationPlan.ExistingIDs, "runtime exclusions must not alter the immutable creation reservation")
}
