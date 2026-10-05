package upstreamgovernance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGovernanceMultipleTargetsUseOneAccountKeyAndBinding(t *testing.T) {
	svc, store, connector, local := setupEngine(t)
	_, err := svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	selected := selections()
	selected[0].LocalGroupIDs = []int64{9, 7, 9}
	preview, err := svc.Preview(t.Context(), 1, selected)
	require.NoError(t, err)
	require.Equal(t, []int64{7, 9}, preview.Rows[0].Selection.LocalGroupIDs)
	require.Len(t, preview.Rows[0].Targets, 2)
	require.EqualValues(t, 7, preview.Rows[0].Target.ID)
	require.Equal(t, 1, *preview.Rows[0].Selection.AccountConfig.Priority)
	result, err := svc.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Len(t, local.accounts, 1)
	require.Equal(t, []int64{7, 9}, local.changes[0].GroupIDs)
	require.Len(t, local.changes[0].ExpectedTargetFingerprints, 2)
	require.Len(t, store.keys, 1)
	require.Len(t, store.bindings, 1)
	require.Equal(t, []int64{7, 9}, store.bindings[0].LocalGroupIDs)
	_, err = svc.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, 1, connector.keyCalls)
	require.Equal(t, 1, local.calls)
	// A fresh preview with a differently ordered set retains the stable account
	// identity and key. Target groups are not part of the remote marker.
	selected[0].LocalGroupIDs = []int64{9, 7}
	second, err := svc.Preview(t.Context(), 1, selected)
	require.NoError(t, err)
	require.Equal(t, preview.Rows[0].Marker, second.Rows[0].Marker)
	require.False(t, second.Rows[0].WillCreateKey)
	result, err = svc.Apply(t.Context(), 1, second.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Len(t, local.accounts, 1)
	require.Len(t, store.keys, 1)
	require.Len(t, store.bindings, 1)
	require.Equal(t, 1, connector.keyCalls)
}

func TestGovernanceValidatesEveryTargetBeforeAnyRemoteEffects(t *testing.T) {
	svc, _, connector, local := setupEngine(t)
	other := connector.catalog.Groups[0]
	other.ID = "9"
	connector.catalog.Groups = append(connector.catalog.Groups, other)
	_, err := svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	selected := selections()
	selected = append(selected, Selection{RemoteGroupID: "9", Platform: "openai", LocalGroupIDs: []int64{8, 9}, CostMultiplier: 1})
	preview, err := svc.Preview(t.Context(), 1, selected)
	require.NoError(t, err)
	local.targetFingerprints = map[int64]string{9: "changed-second-target"}
	_, err = svc.Apply(t.Context(), 1, preview.ID)
	require.ErrorIs(t, err, ErrConflict)
	require.Zero(t, connector.keyCalls)
	require.Zero(t, local.calls)
}

func TestGovernanceMultipleTargetRecoveryComparesTheEntireGroupSet(t *testing.T) {
	svc, store, connector, local := setupEngine(t)
	_, err := svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	selected := selections()
	selected[0].LocalGroupIDs = []int64{7, 9}
	preview, err := svc.Preview(t.Context(), 1, selected)
	require.NoError(t, err)
	store.failBindingAfterAccount = true
	result, err := svc.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Items[0].Status)
	store.failBindingAfterAccount = false
	account := local.accounts[preview.Rows[0].Marker]
	account.GroupIDs = []int64{7}
	_, err = svc.Apply(t.Context(), 1, preview.ID)
	require.ErrorIs(t, err, ErrConflict, "partial local bindings must not count as successful recovery")
	account.GroupIDs = []int64{9, 7}
	result, err = svc.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Equal(t, 1, connector.keyCalls)
	require.Len(t, local.accounts, 1)
}

func TestGovernanceSelectionAndPriorityNormalization(t *testing.T) {
	for _, value := range []struct {
		ids    []int64
		legacy int64
	}{{[]int64{}, 7}, {[]int64{0}, 0}, {[]int64{-1, 7}, 7}, {[]int64{7, 9}, 8}, {make([]int64, 101), 0}} {
		_, err := NormalizeLocalGroupIDs(value.ids, value.legacy)
		require.ErrorIs(t, err, ErrInvalid)
	}
	ids, err := NormalizeLocalGroupIDs(nil, 7)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, ids)
	var omitted AccountConfig
	require.NoError(t, json.Unmarshal([]byte(`{"concurrency":5000}`), &omitted))
	config, err := NormalizeAccountConfig(&omitted, "openai", nil)
	require.NoError(t, err)
	require.Equal(t, 1, *config.Priority)
	zero := 0
	omitted.Priority = &zero
	config, err = NormalizeAccountConfig(&omitted, "openai", nil)
	require.NoError(t, err)
	require.Zero(t, *config.Priority)
	zero = 7
	require.Zero(t, *config.Priority, "preview must own its normalized priority value")
	negative := -1
	omitted.Priority = &negative
	_, err = NormalizeAccountConfig(&omitted, "openai", nil)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestGovernanceOldPendingPreviewRequiresNewReview(t *testing.T) {
	for _, missing := range []string{"priority", "targets"} {
		t.Run(missing, func(t *testing.T) {
			svc, _, connector, local := setupEngine(t)
			_, err := svc.Sync(t.Context(), 1)
			require.NoError(t, err)
			preview, err := svc.Preview(t.Context(), 1, selections())
			require.NoError(t, err)
			if missing == "priority" {
				preview.Rows[0].Selection.AccountConfig.Priority = nil
			} else {
				preview.Rows[0].Targets = nil
			}
			_, err = svc.Apply(t.Context(), 1, preview.ID)
			require.ErrorIs(t, err, ErrConflict)
			require.Zero(t, connector.keyCalls)
			require.Zero(t, local.calls)
		})
	}
}
