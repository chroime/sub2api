package upstreamgovernance

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFastObservationConnectorReadsOnlyCompleteGroupsAndRates(t *testing.T) {
	connector := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case "/api/v1/groups/available":
			return 200, `{"code":0,"data":[{"id":7,"name":"VIP","platform":"openai","rate_multiplier":2}]}`
		case "/api/v1/groups/rates":
			return 200, `{"code":0,"data":{"7":0.8}}`
		default:
			t.Fatalf("fast observation made an unexpected request: %s", r.URL.Path)
			return 500, "{}"
		}
	})
	fast, ok := connector.(FastObservationConnector)
	require.True(t, ok, "production connector must expose the cheap observation path")
	got, err := fast.ObserveGroups(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42})
	require.NoError(t, err)
	require.True(t, got.GroupsComplete)
	require.Equal(t, int64(42), got.SourceUserID)
	require.Len(t, got.Groups, 1)
	require.Equal(t, "VIP", got.Groups[0].Name)
	require.NotNil(t, got.Groups[0].ResolvedRateMultiplier)
	require.InDelta(t, .8, *got.Groups[0].ResolvedRateMultiplier, .0001)
	require.Empty(t, got.Groups[0].Models, "fast observation must not expand models")
	require.Empty(t, got.Groups[0].Prices, "fast observation must not expand prices")
	require.WithinDuration(t, time.Now(), got.ObservedAt, 2*time.Second)
}

func TestFastObservationRejectsIncompleteOrUnknownRates(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "null groups", body: `{"code":0,"data":null}`},
		{name: "unknown rate", body: `{"code":0,"data":{"7":"auto"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/api/v1/user/profile":
					return 200, `{"code":0,"data":{"id":42}}`
				case "/api/v1/groups/available":
					if tc.name == "null groups" {
						return 200, tc.body
					}
					return 200, `{"code":0,"data":[{"id":7,"name":"VIP","platform":"openai","rate_multiplier":2}]}`
				case "/api/v1/groups/rates":
					return 200, tc.body
				default:
					t.Fatalf("unexpected endpoint %s", r.URL.Path)
					return 500, "{}"
				}
			})
			fast := connector.(FastObservationConnector)
			_, err := fast.ObserveGroups(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42})
			require.ErrorIs(t, err, ErrUnsupported)
		})
	}
}
