package upstreamgovernance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateKeysInventoryReauthPersistsBlockedSiteAndSession(t *testing.T) {
	s, store, inventory, _, _ := keyHealthFixture(t)
	runtime := &runtimeMemoryStore{memoryStore: store.memoryStore}
	s.store = &keyHealthRuntimeStore{runtimeMemoryStore: runtime}
	inventory.err = ErrReauth

	result, err := s.CreateKeys(t.Context(), store.site.ID, CreateKeysInput{
		SnapshotID: store.snap.ID,
		Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}},
	})
	require.NoError(t, err)
	require.Equal(t, "reauth_required", result.Items[0].Error)
	require.Equal(t, "reauth_required", store.site.Status)
	require.NotEmpty(t, runtime.saves)
	require.Equal(t, "reauth_required", runtime.saves[len(runtime.saves)-1].RefreshState)
}

func TestApplyInventoryReauthPersistsBlockedSiteAndSession(t *testing.T) {
	s, store, inventory, _, _ := keyHealthFixture(t)
	runtime := &runtimeMemoryStore{memoryStore: store.memoryStore}
	s.store = &keyHealthRuntimeStore{runtimeMemoryStore: runtime}
	preview, err := s.Preview(t.Context(), store.site.ID, selections())
	require.NoError(t, err)
	inventory.err = ErrReauth

	result, err := s.Apply(t.Context(), store.site.ID, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "reauth_required", result.Items[0].Error)
	require.Equal(t, "reauth_required", store.site.Status)
	require.NotEmpty(t, runtime.saves)
	require.Equal(t, "reauth_required", runtime.saves[len(runtime.saves)-1].RefreshState)
}
