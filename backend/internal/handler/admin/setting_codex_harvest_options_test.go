package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingsCodexHarvestOptionsDefaultsPartialSaveAndClear(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	get := func() map[string]any {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
		h.GetSettings(c)
		return codexDualTicketResponse(t, rec)
	}
	data := get()
	for _, prefix := range []string{"openai_codex_ticket_", "openai_codex_ticket_332_"} {
		require.Equal(t, false, data[prefix+"verify_enabled"])
		require.Equal(t, float64(3), data[prefix+"harvest_concurrency"])
		require.Equal(t, []any{}, data[prefix+"harvest_proxy_ids"])
	}
	for _, prefix := range []string{"openai_codex_ticket_", "openai_codex_ticket_332_"} {
		data = codexDualTicketResponse(t, doUpdateSettings(t, h, map[string]any{
			prefix + "verify_enabled":      true,
			prefix + "harvest_concurrency": 5,
			prefix + "harvest_proxy_ids":   []int64{9, 7},
		}, nil))
		require.Equal(t, true, data[prefix+"verify_enabled"])
		require.Equal(t, float64(5), data[prefix+"harvest_concurrency"])
		require.Equal(t, []any{float64(9), float64(7)}, data[prefix+"harvest_proxy_ids"])
	}
	codexDualTicketResponse(t, doUpdateSettings(t, h, map[string]any{"site_name": "preserve ticket options"}, nil))
	for _, prefix := range []string{"openai_codex_ticket_", "openai_codex_ticket_332_"} {
		require.Equal(t, "true", repo.values[prefix+"verify_enabled"])
		require.Equal(t, "5", repo.values[prefix+"harvest_concurrency"])
		require.JSONEq(t, `[9,7]`, repo.values[prefix+"harvest_proxy_ids"])
	}
	data = codexDualTicketResponse(t, doUpdateSettings(t, h, map[string]any{
		"openai_codex_ticket_verify_enabled": false, "openai_codex_ticket_harvest_proxy_ids": []int64{},
	}, nil))
	require.Equal(t, false, data["openai_codex_ticket_verify_enabled"])
	require.Equal(t, []any{}, data["openai_codex_ticket_harvest_proxy_ids"])
	require.Equal(t, true, data["openai_codex_ticket_332_verify_enabled"])
	require.Equal(t, []any{float64(9), float64(7)}, data["openai_codex_ticket_332_harvest_proxy_ids"])
}

func TestSettingsCodexHarvestOptionsInvalidUpdateIsAtomic(t *testing.T) {
	tooMany := make([]int64, 33)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	for _, prefix := range []string{"openai_codex_ticket_", "openai_codex_ticket_332_"} {
		for _, tc := range []struct {
			name, suffix string
			value        any
		}{
			{"zero", "harvest_concurrency", 0}, {"negative", "harvest_concurrency", -1}, {"high", "harvest_concurrency", 17},
			{"invalid id", "harvest_proxy_ids", []int64{0}}, {"negative id", "harvest_proxy_ids", []int64{-1}},
			{"duplicate", "harvest_proxy_ids", []int64{1, 1}}, {"too many", "harvest_proxy_ids", tooMany},
		} {
			t.Run(prefix+tc.name, func(t *testing.T) {
				h, repo := newStepUpSwitchTestHandler(t, map[string]string{"site_name": "unchanged", prefix + "harvest_concurrency": "3"})
				r := doUpdateSettings(t, h, map[string]any{"site_name": "must not save", prefix + tc.suffix: tc.value}, nil)
				require.Equal(t, http.StatusBadRequest, r.Code, r.Body.String())
				require.Equal(t, "unchanged", repo.values["site_name"])
				require.Equal(t, "3", repo.values[prefix+"harvest_concurrency"])
			})
		}
	}
}

func TestSettingsCodexHarvestOptionsNullPreservesExistingValuesAndMaskedProxy(t *testing.T) {
	for _, prefix := range []string{"openai_codex_ticket_", "openai_codex_ticket_332_"} {
		t.Run(prefix, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{
				prefix + "verify_enabled": "true", prefix + "harvest_concurrency": "5", prefix + "harvest_proxy_ids": "[4,7]",
				prefix + "harvest_proxy_url": "http://route-user:route-secret@proxy.example:8080",
			})
			recorder := doUpdateSettings(t, h, map[string]any{
				prefix + "verify_enabled": nil, prefix + "harvest_concurrency": nil, prefix + "harvest_proxy_ids": nil,
				"site_name": "new site name",
			}, nil)
			data := codexDualTicketResponse(t, recorder)
			require.Equal(t, true, data[prefix+"verify_enabled"])
			require.Equal(t, float64(5), data[prefix+"harvest_concurrency"])
			require.Equal(t, []any{float64(4), float64(7)}, data[prefix+"harvest_proxy_ids"])
			require.Equal(t, "http://route-user:route-secret@proxy.example:8080", repo.values[prefix+"harvest_proxy_url"])
			require.NotContains(t, recorder.Body.String(), "route-secret")
		})
	}
}

func TestSettingsCodexHarvestOptionsMixedInvalidModesDoNotPartiallySave(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		"site_name": "unchanged", "openai_codex_ticket_verify_enabled": "false", "openai_codex_ticket_harvest_concurrency": "3",
	})
	recorder := doUpdateSettings(t, h, map[string]any{
		"site_name": "must not save", "openai_codex_ticket_verify_enabled": true,
		"openai_codex_ticket_harvest_concurrency": 9, "openai_codex_ticket_332_harvest_proxy_ids": []int64{4, 4},
	}, nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "unchanged", repo.values["site_name"])
	require.Equal(t, "false", repo.values["openai_codex_ticket_verify_enabled"])
	require.Equal(t, "3", repo.values["openai_codex_ticket_harvest_concurrency"])
}
