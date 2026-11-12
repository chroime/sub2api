package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeGroupIdentity(t *testing.T) {
	for _, test := range []struct {
		name  string
		input GroupIdentity
		want  GroupIdentity
	}{
		{name: "unconfigured", input: GroupIdentity{}, want: GroupIdentity{}},
		{name: "clear label also clears color", input: GroupIdentity{Label: "  ", Color: "rose"}, want: GroupIdentity{}},
		{name: "trim and default color", input: GroupIdentity{Label: "  官方  "}, want: GroupIdentity{Label: "官方", Color: "teal"}},
		{name: "twelve unicode characters", input: GroupIdentity{Label: strings.Repeat("字", 12), Color: "blue"}, want: GroupIdentity{Label: strings.Repeat("字", 12), Color: "blue"}},
		{name: "emoji characters", input: GroupIdentity{Label: "🚀加速", Color: "amber"}, want: GroupIdentity{Label: "🚀加速", Color: "amber"}},
		{name: "plain text stays plain", input: GroupIdentity{Label: "<官方>", Color: "violet"}, want: GroupIdentity{Label: "<官方>", Color: "violet"}},
		{name: "custom hex color", input: GroupIdentity{Label: "旗舰", Color: "#A1B2C3"}, want: GroupIdentity{Label: "旗舰", Color: "#a1b2c3"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeGroupIdentity(test.input)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
	for _, color := range []string{"teal", "blue", "amber", "violet", "rose", "gray"} {
		got, err := NormalizeGroupIdentity(GroupIdentity{Label: "自定义", Color: color})
		require.NoError(t, err)
		require.Equal(t, color, got.Color)
	}
}

func TestNormalizeGroupIdentityRejectsInvalidPresentation(t *testing.T) {
	for _, test := range []GroupIdentity{
		{Label: strings.Repeat("字", 13)},
		{Label: "一行\n两行"},
		{Label: "\r官方"},
		{Label: "官方\t"},
		{Label: "官方\x00"},
		{Label: "官方\u0085"},
		{Label: "官方\u2028标识"},
		{Label: "官方\u2029标识"},
		{Label: string([]byte{0xff})},
		{Label: "官方", Color: "red"},
		{Label: "官方", Color: "TEAL"},
		{Label: "官方", Color: "teal\n"},
		{Label: "官方", Color: "#abc"},
		{Label: "官方", Color: "#aabbccdd"},
		{Label: "官方", Color: "#gg1122"},
		{Label: "官方", Color: "rgb(1, 2, 3)"},
		{Label: "官方", Color: "url(https://example.test)"},
		{Color: "unknown"},
	} {
		_, err := NormalizeGroupIdentity(test)
		require.Error(t, err, "%+v", test)
	}
}

func TestGroupIdentityZeroValueSerializesAsEmptyObject(t *testing.T) {
	raw, err := json.Marshal(GroupIdentity{})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(raw))
	raw, err = json.Marshal(GroupIdentity{Label: "官方", Color: "teal"})
	require.NoError(t, err)
	var decoded GroupIdentity
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, GroupIdentity{Label: "官方", Color: "teal"}, decoded)
}
