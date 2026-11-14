//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorFollowsLocalKeyGroupInsteadOfDisplayName(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	first, err := repo.client.Group.Create().SetName("same-name").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	second, err := repo.client.Group.Create().SetName("same-name").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	user, err := repo.client.User.Create().SetEmail("monitor-binding@example.test").SetPasswordHash("unused").Save(ctx)
	require.NoError(t, err)
	key, err := repo.client.APIKey.Create().SetUserID(user.ID).SetName("probe").SetKey("sk-local-monitor-binding").SetGroupID(first.ID).Save(ctx)
	require.NoError(t, err)
	svc := service.NewChannelMonitorService(repo, channelMonitorSortTestEncryptor{})
	m, err := svc.Create(ctx, service.ChannelMonitorCreateParams{
		Name: "independent card title", Provider: "openai", Endpoint: "https://example.com",
		APIKey: key.Key, PrimaryModel: "test-model", GroupName: "unrelated label", GroupID: &second.ID, Enabled: true, IntervalSeconds: 60,
	})
	require.NoError(t, err)
	require.Equal(t, first.Name, m.GroupName, "local key determines the group, regardless of display names")

	require.NoError(t, repo.client.Group.UpdateOneID(first.ID).SetName("renamed group").Exec(ctx))
	m, err = svc.Get(ctx, m.ID)
	require.NoError(t, err)
	require.Equal(t, "renamed group", m.GroupName)

	oldCheck := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, repo.InsertHistoryBatch(ctx, []*service.ChannelMonitorHistoryRow{{
		MonitorID: m.ID, Model: m.PrimaryModel, Status: "operational", CheckedAt: oldCheck,
	}}))
	require.NoError(t, repo.client.APIKey.UpdateOneID(key.ID).SetGroupID(second.ID).Exec(ctx))
	m, err = svc.Get(ctx, m.ID)
	require.NoError(t, err)
	require.Equal(t, second.Name, m.GroupName)
	require.NotNil(t, m.GroupID)
	require.Equal(t, second.ID, *m.GroupID)
	require.NotNil(t, m.GroupBindingChangedAt)
	stored, err := repo.GetByID(ctx, m.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.GroupBindingChangedAt)
	history, err := svc.ListHistory(ctx, m.ID, "", 10)
	require.NoError(t, err)
	require.Empty(t, history, "the old group's probe must not appear under the new group")
	count, err := repo.client.ChannelMonitorHistory.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "rebinding must retain stored history")

	require.NoError(t, repo.client.APIKey.UpdateOneID(key.ID).SetDeletedAt(time.Now()).Exec(ctx))
	m, err = svc.Get(ctx, m.ID)
	require.NoError(t, err)
	require.Empty(t, m.GroupName, "deleted keys must never fall back to a same-named group")
	require.Equal(t, "key_missing", m.GroupBindingStatus)
	require.Equal(t, key.ID, *m.LocalAPIKeyID)
	require.Nil(t, m.GroupID)
	checkMode, disabled, zero := "probe", false, int64(0)
	m, err = svc.Update(ctx, m.ID, service.ChannelMonitorUpdateParams{CheckMode: &checkMode, Enabled: &disabled, GroupID: &zero})
	require.NoError(t, err)
	require.Equal(t, service.MonitorGroupBindingKey, m.GroupBindingMode, "editing another field must not turn a deleted local key into an external key")
	require.Equal(t, key.ID, *m.LocalAPIKeyID)
}

func TestChannelMonitorExternalKeyManualGroupAndValidation(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	g, err := repo.client.Group.Create().SetName("external group").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	other, err := repo.client.Group.Create().SetName("other platform").SetPlatform("gemini").Save(ctx)
	require.NoError(t, err)
	svc := service.NewChannelMonitorService(repo, channelMonitorSortTestEncryptor{})
	p := service.ChannelMonitorCreateParams{
		Name: "external probe", Provider: "openai", Endpoint: "https://8.8.8.8",
		APIKey: "external-credential", PrimaryModel: "model", Enabled: true, IntervalSeconds: 60, GroupID: &g.ID,
	}
	m, err := svc.Create(ctx, p)
	require.NoError(t, err)
	require.Equal(t, service.MonitorGroupBindingManual, m.GroupBindingMode)
	require.Equal(t, g.ID, *m.GroupID)
	require.Nil(t, m.LocalAPIKeyID)
	p.GroupID = &other.ID
	_, err = svc.Create(ctx, p)
	require.ErrorIs(t, err, service.ErrChannelMonitorGroupBinding)
	missing := int64(9999999)
	p.GroupID = &missing
	_, err = svc.Create(ctx, p)
	require.ErrorIs(t, err, service.ErrChannelMonitorGroupBinding)
	zero := int64(0)
	m, err = svc.Update(ctx, m.ID, service.ChannelMonitorUpdateParams{GroupID: &zero})
	require.NoError(t, err)
	require.Nil(t, m.GroupID)
	require.Empty(t, m.GroupName)
	require.Equal(t, service.MonitorGroupBindingNone, m.GroupBindingMode)
}

func TestChannelMonitorLegacyBindingDoesNotGuessDuplicateNames(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, err := repo.client.Group.Create().SetName("duplicate").SetPlatform("openai").Save(ctx)
		require.NoError(t, err)
	}
	m := createChannelMonitorSortFixture(t, repo, "legacy", true)
	m.GroupName = "duplicate"
	require.NoError(t, repo.Update(ctx, m))
	svc := service.NewChannelMonitorService(repo, channelMonitorSortTestEncryptor{})
	m, err := svc.Get(ctx, m.ID)
	require.NoError(t, err)
	require.Nil(t, m.GroupID)
	require.Equal(t, service.MonitorGroupBindingNone, m.GroupBindingMode)
	require.Empty(t, m.GroupName)
}

func TestChannelMonitorLegacyUniqueIDPreservesHistoryAndResetUntilIdentityChanges(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	g, err := repo.client.Group.Create().SetName("legacy group").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	m := createChannelMonitorSortFixture(t, repo, "legacy", true)
	m.GroupID, m.GroupName = &g.ID, g.Name
	m.AvailabilityReset = &service.ChannelMonitorAvailabilityReset{Version: 1, Model: m.PrimaryModel, TargetPct: 100, ResetAt: time.Now().UTC(), BaselineTotal: 60, BaselineOK: 60}
	require.NoError(t, repo.Update(ctx, m))
	svc := service.NewChannelMonitorService(repo, channelMonitorSortTestEncryptor{})
	m, err = svc.Get(ctx, m.ID)
	require.NoError(t, err)
	require.Equal(t, service.MonitorGroupBindingManual, m.GroupBindingMode)
	require.Nil(t, m.GroupBindingChangedAt)
	require.NotNil(t, m.AvailabilityReset)
	zero := int64(0)
	m, err = svc.Update(ctx, m.ID, service.ChannelMonitorUpdateParams{GroupID: &zero})
	require.NoError(t, err)
	require.NotNil(t, m.GroupBindingChangedAt)
	require.Nil(t, m.AvailabilityReset)
	m, err = svc.Get(ctx, m.ID)
	require.NoError(t, err)
	require.Nil(t, m.AvailabilityReset)
}

type monitorBindingConflictRepository struct {
	*channelMonitorRepository
	conflict bool
}

func (r *monitorBindingConflictRepository) SaveMonitorGroupBinding(ctx context.Context, m *service.ChannelMonitor, before service.MonitorGroupBindingIdentity) (bool, error) {
	if r.conflict {
		r.conflict = false
		return false, nil
	}
	return r.channelMonitorRepository.SaveMonitorGroupBinding(ctx, m, before)
}

func TestChannelMonitorBindingRefreshRetriesConcurrentIdentityChange(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	m := createChannelMonitorSortFixture(t, repo, "concurrent initialization", true)
	svc := service.NewChannelMonitorService(&monitorBindingConflictRepository{repo, true}, channelMonitorSortTestEncryptor{})
	resolved, err := svc.Get(context.Background(), m.ID)
	require.NoError(t, err, "concurrent initialization should reload rather than fail the whole status page")
	require.Equal(t, m.ID, resolved.ID)
	require.Equal(t, service.MonitorGroupBindingNone, resolved.GroupBindingMode)
}

type monitorUndecryptableKey struct {
	channelMonitorSortTestEncryptor
}

func (monitorUndecryptableKey) Decrypt(string) (string, error) {
	return "", errors.New("unavailable encryption key")
}

func TestChannelMonitorLegacyUnreadableKeyRetainsExplicitIDAndFollowsGroupRename(t *testing.T) {
	repo, _ := newChannelMonitorSortRepository(t)
	ctx := context.Background()
	g, err := repo.client.Group.Create().SetName("renamed legacy group").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	m := createChannelMonitorSortFixture(t, repo, "legacy encrypted credential", true)
	m.GroupID, m.GroupName = &g.ID, "old group name"
	require.NoError(t, repo.Update(ctx, m))
	svc := service.NewChannelMonitorService(repo, monitorUndecryptableKey{})
	m, err = svc.Get(ctx, m.ID)
	require.NoError(t, err)
	require.Equal(t, g.Name, m.GroupName)
	require.Equal(t, g.ID, *m.GroupID)
	require.True(t, m.APIKeyDecryptFailed)
	require.Equal(t, service.MonitorGroupBindingLegacy, m.GroupBindingMode, "key discovery must remain retryable")
}
