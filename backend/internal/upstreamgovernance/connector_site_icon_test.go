package upstreamgovernance

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const fixtureSiteIcon = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a7HkAAAAASUVORK5CYII="

type siteIconReader interface {
	SiteIcon(context.Context, Site) (string, error)
}

func TestGovernanceSiteIconUsesPublicBrandingWithoutLogin(t *testing.T) {
	for _, platform := range []string{"sub2api", "newapi"} {
		t.Run(platform, func(t *testing.T) {
			proxy := int64(47)
			calls := 0
			connector := NewConnector(func(_ context.Context, site Site) (HTTPDoer, error) {
				require.Equal(t, &proxy, site.ProxyID)
				return connectorDoer(func(r *http.Request) (*http.Response, error) {
					calls++
					require.Equal(t, "GET", r.Method)
					require.Empty(t, r.Header.Get("Authorization"))
					require.Empty(t, r.Header.Get("Cookie"))
					require.Equal(t, "upstream.example", r.URL.Host)
					_, hasDeadline := r.Context().Deadline()
					require.True(t, hasDeadline)
					body := `{"code":0,"data":{"site_favicon":"` + fixtureSiteIcon + `"}}`
					if platform == "newapi" {
						body = `{"success":true,"data":{"logo":"` + fixtureSiteIcon + `"}}`
					}
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
				}), nil
			})
			reader, ok := connector.(siteIconReader)
			require.True(t, ok, "the connector must discover site-specific branding")
			site := Site{ID: 1, Version: 1, Name: "Fixture", Platform: platform, BaseURL: "https://upstream.example", ProxyID: &proxy}
			icon, err := reader.SiteIcon(t.Context(), site)
			require.NoError(t, err)
			require.Equal(t, fixtureSiteIcon, icon)
			require.Equal(t, 1, calls)
			_, err = reader.SiteIcon(t.Context(), site)
			require.NoError(t, err)
			require.Equal(t, 1, calls, "repeated rail and picker renders share a cached icon")
		})
	}
}

func TestGovernanceSiteIconFindsHTMLDeclaredIconAndRejectsHTMLFavicon(t *testing.T) {
	for _, useHTMLIcon := range []bool{true, false} {
		t.Run(map[bool]string{true: "declared SVG", false: "HTML masquerading as favicon"}[useHTMLIcon], func(t *testing.T) {
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/api/status":
					return 200, `{"success":true,"data":{"logo":""}}`
				case "/":
					if useHTMLIcon {
						return 200, `<html><head><link href="/brand/icon.svg" rel="icon"></head></html>`
					}
					return 200, `<html><head></head></html>`
				case "/brand/icon.svg":
					return 200, `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h1v1z"/></svg>`
				case "/favicon.ico":
					return 200, `<html><body>SPA fallback</body></html>`
				}
				return 404, `{}`
			})
			reader, ok := connector.(siteIconReader)
			require.True(t, ok)
			icon, err := reader.SiteIcon(t.Context(), Site{Name: "Fixture", Platform: "newapi", BaseURL: "https://upstream.example"})
			if useHTMLIcon {
				require.NoError(t, err)
				require.True(t, strings.HasPrefix(icon, "data:image/svg+xml;base64,"))
			} else {
				require.Error(t, err)
				require.Empty(t, icon)
			}
		})
	}
}

func TestGovernanceSiteIconValidatesSourcesBeforeFetching(t *testing.T) {
	for _, source := range []string{"http://private.example/icon.png", "https://127.0.0.1/icon.png", "https://localhost/icon.png", "https://user:password@public.example/icon.png", "javascript:alert(1)", "data:text/html;base64,PHNjcmlwdD4="} {
		t.Run(source, func(t *testing.T) {
			calls := 0
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				calls++
				if r.URL.Path == "/api/status" {
					return 200, `{"success":true,"data":{"logo":"` + source + `"}}`
				}
				if r.URL.Path == "/favicon.ico" {
					png, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(fixtureSiteIcon, "data:image/png;base64,"))
					require.NoError(t, err)
					return 200, string(png)
				}
				return 404, `{}`
			})
			reader, ok := connector.(siteIconReader)
			require.True(t, ok)
			icon, err := reader.SiteIcon(t.Context(), Site{Name: "Fixture", Platform: "newapi", BaseURL: "https://upstream.example"})
			require.NoError(t, err)
			require.Equal(t, fixtureSiteIcon, icon)
			require.LessOrEqual(t, calls, 3)
		})
	}
}
