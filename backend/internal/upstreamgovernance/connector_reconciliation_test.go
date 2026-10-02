package upstreamgovernance

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorReconciliationRequiresAuthoritativeRates(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		invalid   bool
		want      float64
	}{{"null document", "null", true, 0}, {"null override", `{"7":null}`, true, 0}, {"zero override", `{"7":0}`, false, 0}, {"no override", `{}`, false, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/api/v1/user/profile":
					return 200, `{"code":0,"data":{"id":42}}`
				case "/api/v1/groups/available":
					return 200, `{"code":0,"data":[{"id":7,"name":"VIP","platform":"openai","rate_multiplier":2}]}`
				case "/api/v1/groups/rates":
					return 200, `{"code":0,"data":` + tc.raw + `}`
				default:
					return 404, `{}`
				}
			})
			catalog, err := connector.Discover(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "test", UserID: 42})
			if tc.invalid {
				require.ErrorIs(t, err, ErrUnsupported)
				return
			}
			require.NoError(t, err)
			require.True(t, catalog.GroupsComplete)
			require.Equal(t, tc.want, *catalog.Groups[0].ResolvedRateMultiplier)
		})
	}
}
