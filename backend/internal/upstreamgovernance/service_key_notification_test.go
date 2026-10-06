package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type keyNoticeFixture struct {
	to      []string
	notices []KeyNotice
	sentTo  []string
	fail    bool
	failTo  string
}

func (n *keyNoticeFixture) Recipients(context.Context, []string) ([]string, error) {
	return append([]string(nil), n.to...), nil
}
func (n *keyNoticeFixture) SendKey(_ context.Context, recipient string, notice KeyNotice) error {
	n.notices = append(n.notices, notice)
	n.sentTo = append(n.sentTo, recipient)
	if n.fail || recipient == n.failTo {
		return errors.New("fixture SMTP failure")
	}
	return nil
}

func TestKeyMailRetrySkipsRecipientsAlreadyNotified(t *testing.T) {
	s, store, inventory, _, now := keyProtectionFixture(t, false)
	notifier := &keyNoticeFixture{to: []string{"first@example.test", "second@example.test"}, failTo: "second@example.test"}
	s.SetKeyNotifier(notifier)
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"first@example.test", "second@example.test"}, notifier.sentTo)
	require.Equal(t, "failed", store.keys[0].Health.NotificationStatus)
	notifier.failTo = ""
	*now = now.Add(15 * time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"first@example.test", "second@example.test", "second@example.test"}, notifier.sentTo)
	require.Equal(t, "sent", store.keys[0].Health.NotificationStatus)
}

func TestKeyMissingAndRecoverySendOneEmailPerIncident(t *testing.T) {
	s, store, inventory, _, now := keyProtectionFixture(t, false)
	notifier := &keyNoticeFixture{to: []string{"admin@example.test"}}
	s.SetKeyNotifier(notifier)
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Empty(t, notifier.notices)
	*now = now.Add(time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Len(t, notifier.notices, 1)
	require.Equal(t, "missing", notifier.notices[0].Kind)
	require.Equal(t, "8", notifier.notices[0].RemoteGroupID)
	require.Equal(t, "fixture", notifier.notices[0].RemoteKeyID)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Len(t, notifier.notices, 1)
	inventory.records = []RemoteKeyIdentity{{ID: "fixture", GroupID: "8"}}
	*now = now.Add(time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Len(t, notifier.notices, 2)
	require.Equal(t, "recovered", notifier.notices[1].Kind)
}

func TestKeyEmailFailureKeepsHealthAndRetriesAfterCooldown(t *testing.T) {
	s, store, inventory, _, now := keyProtectionFixture(t, false)
	notifier := &keyNoticeFixture{to: []string{"admin@example.test"}, fail: true}
	s.SetKeyNotifier(notifier)
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err, "SMTP failure must not erase a confirmed key observation")
	require.Equal(t, "confirmed_missing", store.keys[0].Health.Status)
	require.Equal(t, "failed", store.keys[0].Health.NotificationStatus)
	require.Len(t, notifier.notices, 1)
	*now = now.Add(5 * time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Len(t, notifier.notices, 1)
	notifier.fail = false
	*now = now.Add(15 * time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Len(t, notifier.notices, 2)
	require.Equal(t, "sent", store.keys[0].Health.NotificationStatus)
}

func TestFailedKeyMailRunsFromSchedulerBeforeNextCatalog(t *testing.T) {
	s, store, inventory, _, now := keyProtectionFixture(t, false)
	notifier := &keyNoticeFixture{to: []string{"admin@example.test"}, fail: true}
	s.SetKeyNotifier(notifier)
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", store.keys[0].Health.NotificationStatus)
	require.Equal(t, now.Add(15*time.Minute), *store.keys[0].Health.NextCheckAt)
	store.site.NextSyncAt = now.Add(60 * time.Minute)
	before := inventory.discoveryCalls
	notifier.fail = false
	*now = now.Add(15 * time.Minute)
	require.NoError(t, s.runSiteDue(t.Context(), store.site.ID))
	require.Equal(t, before, inventory.discoveryCalls)
	require.Equal(t, "sent", store.keys[0].Health.NotificationStatus)
	require.Nil(t, store.keys[0].Health.NextCheckAt)
}

func TestFailedKeyMailRetriesWithoutAHealthyInventory(t *testing.T) {
	s, store, inventory, _, now := keyProtectionFixture(t, false)
	notifier := &keyNoticeFixture{to: []string{"admin@example.test"}, fail: true}
	s.SetKeyNotifier(notifier)
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", store.keys[0].Health.NotificationStatus)
	require.Len(t, notifier.notices, 1)

	inventory.err = ErrUnsupported
	notifier.fail = false
	store.site.NextSyncAt = now.Add(time.Hour)
	*now = now.Add(15 * time.Minute)
	require.NoError(t, s.runSiteDue(t.Context(), store.site.ID))
	require.Equal(t, KeyHealthConfirmedMissing, store.keys[0].Health.Status)
	require.Equal(t, "unsupported_contract", store.keys[0].Health.ErrorCode)
	require.Len(t, notifier.notices, 2, "a confirmed alert can retry without fresh inventory")
	require.Equal(t, "sent", store.keys[0].Health.NotificationStatus)
	require.NotNil(t, store.keys[0].Health.NextCheckAt)
	require.Equal(t, now.Add(5*time.Minute), *store.keys[0].Health.NextCheckAt, "inventory still needs a bounded retry")
}
