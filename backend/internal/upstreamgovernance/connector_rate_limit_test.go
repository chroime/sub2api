package upstreamgovernance

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFastObservationPreservesBoundedRetryAfter(t *testing.T) {
	connector := fixtureConnector(t, func(*http.Request) (int, string) {
		return http.StatusTooManyRequests, `{"message":"slow down"}`
	})
	fast := connector.(FastObservationConnector)
	_, err := fast.ObserveGroups(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42})
	var limited *RateLimitError
	require.ErrorAs(t, err, &limited)
	require.Equal(t, http.StatusTooManyRequests, limited.StatusCode)
	require.Equal(t, 30*time.Second, limited.RetryAfter)
	require.False(t, errors.Is(err, ErrUnsupported), "rate limiting is retryable, not an invalid contract")
}
