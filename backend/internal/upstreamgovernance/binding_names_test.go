package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type bindingNamesLocal struct {
	LocalAccounts
	names map[int64]LocalAccountName
	ids   []int64
	calls int
	err   error
}

func (l *bindingNamesLocal) AccountNames(_ context.Context, ids []int64) (map[int64]LocalAccountName, error) {
	l.calls++
	l.ids = append([]int64(nil), ids...)
	return l.names, l.err
}

func TestBindingsReadCurrentAccountNamesInOneBatch(t *testing.T) {
	store := &memoryStore{bindings: []Binding{
		{ID: 1, AccountID: 8, KeyCipher: "private-fixture-key"},
		{ID: 2, AccountID: 8},
		{ID: 3, AccountID: 9},
		{ID: 4, AccountID: 0},
		{ID: 5, AccountID: 10},
	}}
	local := &bindingNamesLocal{names: map[int64]LocalAccountName{8: {Name: "Current account"}, 10: {Name: "Historical account", Deleted: true}}}
	svc := NewService(store, nil, local, nil, false)
	bindings, err := svc.Bindings(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, local.calls)
	require.Equal(t, []int64{8, 9, 10}, local.ids)
	require.Equal(t, "Current account", bindings[0].AccountName)
	require.Equal(t, "Current account", bindings[1].AccountName)
	require.Empty(t, bindings[2].AccountName, "missing accounts keep their ID without an invented name")
	require.Empty(t, bindings[3].AccountName)
	require.Equal(t, "Historical account", bindings[4].AccountName)
	require.True(t, bindings[4].AccountDeleted)
	require.False(t, bindings[0].AccountDeleted)
	require.Empty(t, store.bindings[0].AccountName, "reading names must not write display metadata to stored bindings")
	raw, err := json.Marshal(bindings)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"account_name":"Current account"`)
	require.NotContains(t, string(raw), "private-fixture-key")
	local.names[8] = LocalAccountName{Name: "Manually renamed account"}
	bindings, err = svc.Bindings(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "Manually renamed account", bindings[0].AccountName)
}

func TestBindingsSkipPendingNamesAndSurfaceLookupFailure(t *testing.T) {
	store := &memoryStore{bindings: []Binding{{ID: 1, AccountID: 0}}}
	local := &bindingNamesLocal{}
	svc := NewService(store, nil, local, nil, false)
	bindings, err := svc.Bindings(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Zero(t, local.calls)
	store.bindings[0].AccountID = 8
	local.err = errors.New("name lookup unavailable")
	bindings, err = svc.Bindings(t.Context(), 1)
	require.ErrorIs(t, err, local.err)
	require.Nil(t, bindings)
}
