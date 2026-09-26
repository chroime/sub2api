package upstreamgovernance

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestNativeSiteDetection(t *testing.T) {
	for _, platform := range []string{"sub2api", "newapi"} {
		t.Run(platform, func(t *testing.T) {
			calls := 0
			c := fixtureConnector(t, func(r *http.Request) (int, string) {
				calls++
				if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatal("detection sent credentials or wrote remote state")
				}
				switch r.URL.Path {
				case "/api/v1/settings/public":
					if platform == "sub2api" {
						return 200, `{"code":0,"data":{"site_name":"Fixture","turnstile_enabled":true,"turnstile_site_key":"public-site-key","internal":"secret-canary"}}`
					}
					return 404, `{}`
				case "/api/status":
					return 200, `{"success":true,"data":{"system_name":"Fixture","turnstile_check":true,"turnstile_site_key":"public-site-key"}}`
				default:
					t.Fatalf("unexpected %s", r.URL.Path)
					return 500, ""
				}
			})
			s := NewService(nil, c, nil, nil, false)
			detected, e := s.Detect(context.Background(), DetectInput{BaseURL: "https://upstream.example/"})
			if e != nil {
				t.Fatal(e)
			}
			if detected.Platform != platform || detected.Name != "Fixture" || !detected.CaptchaRequired || detected.BaseURL != "https://upstream.example" {
				t.Fatalf("wrong detection: %+v", detected)
			}
			if platform == "sub2api" && calls != 1 || platform == "newapi" && calls != 2 {
				t.Fatal("unnecessary probing")
			}
		})
	}
}

func TestNativeSiteDetectionRejectsUnsafeOriginsAndUnknownContracts(t *testing.T) {
	calls := 0
	c := fixtureConnector(t, func(*http.Request) (int, string) { calls++; return 200, `{"code":0,"success":true,"data":{}}` })
	s := NewService(nil, c, nil, nil, false)
	for _, origin := range []string{"http://upstream.example", "https://user:password@upstream.example", "https://127.0.0.1", "https://upstream.example/path"} {
		_, e := s.Detect(context.Background(), DetectInput{BaseURL: origin})
		if !errors.Is(e, ErrInvalid) {
			t.Fatalf("unsafe origin result %v", e)
		}
	}
	if calls != 0 {
		t.Fatal("unsafe origin reached HTTP")
	}
	_, e := s.Detect(context.Background(), DetectInput{BaseURL: "https://upstream.example"})
	if !errors.Is(e, ErrUnsupported) {
		t.Fatalf("ambiguous contract accepted: %v", e)
	}
}
