//go:build integration

package accountquality

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQualityPostgresUnbindPreservesOtherBindingsAndEvidence(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	p := f.policy(t, nil)
	b := f.bind(t, account, p)
	oldRun := f.finish(t, b, "pass")
	other := f.bind(t, account, f.policy(t, func(p *Policy) { p.Name = "other policy" }))
	for _, binding := range []Binding{b, other} {
		current := f.binding(t, binding.ID)
		_, err := f.store.Control(t.Context(), current.ID, ControlInput{Version: current.Version, Action: "isolate", Reason: "fixture isolation"}, 42)
		require.NoError(t, err)
	}
	_, err := f.db.ExecContext(t.Context(), "UPDATE accounts SET schedulable=FALSE,temp_unschedulable_reason='manual hold' WHERE id=$1", account)
	require.NoError(t, err)
	b = f.binding(t, b.ID)
	_, err = f.store.Unbind(t.Context(), b.ID, b.Version, 42)
	require.NoError(t, err)
	bindings, err := f.store.Bindings(t.Context(), account)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, other.ID, bindings[0].ID)
	gates := f.gates(t, account)
	require.NotContains(t, gates, fmt.Sprint(b.ID))
	require.Contains(t, gates, fmt.Sprint(other.ID))
	var schedulable bool
	var reason string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT schedulable,temp_unschedulable_reason FROM accounts WHERE id=$1", account).Scan(&schedulable, &reason))
	require.False(t, schedulable)
	require.Equal(t, "manual hold", reason)
	saved, err := f.store.Run(t.Context(), oldRun.ID)
	require.NoError(t, err)
	require.Equal(t, oldRun.ResponseText, saved.ResponseText)
	actions, err := f.store.Actions(t.Context(), account)
	require.NoError(t, err)
	require.Contains(t, actions, requireUnbindAction(t, actions, b.ID))
	_, err = f.store.Queue(t.Context(), b.ID, b.Version, 42, "removed-binding", fixtureEstimate)
	require.ErrorIs(t, err, ErrNotFound)
	rebound := f.bind(t, account, p)
	require.NotEqual(t, b.ID, rebound.ID)
	require.False(t, rebound.Isolated)
	require.NotEmpty(t, f.refreshed)
}

func requireUnbindAction(t *testing.T, actions []Action, bindingID int64) Action {
	t.Helper()
	for _, action := range actions {
		if action.BindingID == bindingID && action.Action == "unbound" {
			require.EqualValues(t, 42, action.ActorID)
			return action
		}
	}
	t.Fatal("missing unbind audit record")
	return Action{}
}

func TestQualityPostgresUnbindRejectsStaleVersionWithoutCancelling(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	b := f.bind(t, account, f.policy(t, nil))
	r := f.claim(t, b)
	_, err := f.store.Unbind(t.Context(), b.ID, b.Version+1, 42)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, b.ID, f.binding(t, b.ID).ID)
	saved, err := f.store.Run(t.Context(), r.ID)
	require.NoError(t, err)
	require.False(t, saved.CancelRequested)
	require.True(t, f.store.Authorize(t.Context(), r))
}

func TestQualityPostgresUnbindCancelsQueuedChecks(t *testing.T) {
	f := newQualityPostgresFixture(t)
	b := f.bind(t, f.account(t), f.policy(t, nil))
	r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, "queued-unbind", fixtureEstimate)
	require.NoError(t, err)
	_, err = f.store.Unbind(t.Context(), b.ID, b.Version, 42)
	require.NoError(t, err)
	saved, err := f.store.Run(t.Context(), r.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", saved.ExecutionStatus)
	require.Equal(t, "not_evaluated", saved.QualityVerdict)
	_, err = f.store.Claim(t.Context())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestQualityPostgresUnbindCancelsRunningChecksAndKeepsLease(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	p := f.policy(t, nil)
	b := f.bind(t, account, p)
	r := f.claim(t, b)
	svc := NewService(f.store, nil, f.store.Capabilities)
	cancelled := false
	svc.active[r.ID] = func() { cancelled = true }
	require.NoError(t, svc.Unbind(t.Context(), b.ID, b.Version, 42))
	require.True(t, cancelled)
	require.False(t, f.store.Authorize(t.Context(), r))
	saved, err := f.store.Run(t.Context(), r.ID)
	require.NoError(t, err)
	require.True(t, saved.CancelRequested)
	require.Equal(t, "running", saved.ExecutionStatus)
	rebound := f.bind(t, account, p)
	_, err = f.store.Queue(t.Context(), rebound.ID, rebound.Version, 42, "rebound-unbind", fixtureEstimate)
	require.ErrorIs(t, err, ErrBusy)
	require.NoError(t, f.store.Complete(t.Context(), fixtureResult(r, "fail")))
	require.Empty(t, f.gates(t, account))
	require.Equal(t, "untested", f.binding(t, rebound.ID).State)
	saved, err = f.store.Run(t.Context(), r.ID)
	require.NoError(t, err)
	require.True(t, saved.Stale)
	require.Equal(t, "cancelled", saved.ExecutionStatus)
	_, err = f.store.Queue(t.Context(), rebound.ID, rebound.Version, 42, "rebound-after-unbind", fixtureEstimate)
	require.NoError(t, err)
}

func TestQualityUnbindValidatesBeforeStorage(t *testing.T) {
	store := &Store{}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, values := range [][3]int64{{0, 1, 42}, {1, 0, 42}, {1, 1, 0}} {
		_, err := store.Unbind(ctx, values[0], values[1], values[2])
		require.ErrorIs(t, err, ErrInvalid)
	}
}
