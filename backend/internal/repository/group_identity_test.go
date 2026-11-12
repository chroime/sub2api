package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupEntityIdentityRoundTripIsIndependentPresentation(t *testing.T) {
	entity := &dbent.Group{ID: 7, Name: "原分组", Platform: "grok", IsExclusive: true,
		Identity: domain.GroupIdentity{Label: "官方", Color: "#a1b2c3"}}
	group := groupEntityToService(entity)
	require.Equal(t, entity.Identity, group.Identity)
	require.Equal(t, entity.Name, group.Name)
	require.Equal(t, entity.Platform, group.Platform)
	require.True(t, group.IsExclusive)
	group.Identity.Label = "自定义"
	require.Equal(t, "官方", entity.Identity.Label, "display metadata must not alias the entity")
	require.Empty(t, groupEntityToService(&dbent.Group{Name: "官方分组", Platform: "openai"}).Identity,
		"unconfigured identity must never be inferred from a group name or provider")
}

func TestGroupIdentityRepositoryRejectsInvalidBeforePersistence(t *testing.T) {
	group := &service.Group{Name: "原分组", Identity: service.GroupIdentity{Label: "invalid\nlabel"}}
	require.ErrorContains(t, createGroupRecord(context.Background(), nil, group), "invalid group identity")
	require.ErrorContains(t, (&groupRepository{}).Update(context.Background(), group), "invalid group identity")
}
