package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupDTOIncludesPublicIdentityWithoutPrivatePolicies(t *testing.T) {
	group := profitControlServiceGroup()
	require.NoError(t, json.Unmarshal([]byte(`{"identity":{"label":"自营","color":"teal"}}`), group))
	for _, result := range []any{GroupFromService(group), GroupFromServiceShallow(group), GroupFromServiceAdmin(group)} {
		fields := marshalToMap(t, result)
		require.Equal(t, map[string]any{"label": "自营", "color": "teal"}, fields["identity"])
	}
	for _, field := range profitControlJSONFields {
		require.NotContains(t, marshalToMap(t, GroupFromService(group)), field)
	}
	var zero service.Group
	fields := marshalToMap(t, GroupFromService(&zero))
	require.Equal(t, map[string]any{}, fields["identity"], "existing groups must have an empty badge by default")
}

func TestGroupDTONestedIdentitySurvivesLocalization(t *testing.T) {
	group := profitControlServiceGroup()
	require.NoError(t, json.Unmarshal([]byte(`{"identity":{"label":"自营","color":"#123abc"}}`), group))
	key := APIKeyFromService(&service.APIKey{Group: group}).Localized("en")
	usage := UsageLogFromService(&service.UsageLog{
		Group: group, APIKey: &service.APIKey{Group: group}, Subscription: &service.UserSubscription{Group: group},
	}).Localized("en")
	for _, nested := range []*Group{key.Group, usage.Group, usage.APIKey.Group, usage.Subscription.Group} {
		require.NotNil(t, nested)
		fields := marshalToMap(t, nested)
		require.Equal(t, map[string]any{"label": "自营", "color": "#123abc"}, fields["identity"])
		require.NotContains(t, fields, "i18n")
		for _, private := range profitControlJSONFields {
			require.NotContains(t, fields, private)
		}
	}
}
