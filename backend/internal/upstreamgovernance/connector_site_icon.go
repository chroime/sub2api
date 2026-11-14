package upstreamgovernance

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/sync/singleflight"
)

const maxSiteIconBytes = 512 << 10

type siteIconCacheEntry struct {
	dataURL string
	err     error
	expires time.Time
}

type siteIconCache struct {
	mu      sync.Mutex
	entries map[string]siteIconCacheEntry
	flight  singleflight.Group
}

// SiteIcon uses only public branding endpoints and the site's explicit proxy.
// No login session, cookies, or API keys are sent to an image source.
func (c *platformConnector) SiteIcon(ctx context.Context, site Site) (string, error) {
	if err := validateSite(&site); err != nil {
		return "", err
	}
	proxy := "direct"
	if site.ProxyID != nil {
		proxy = strconv.FormatInt(*site.ProxyID, 10)
	}
	key := site.BaseURL + "|" + site.Platform + "|" + proxy + "|" + strconv.FormatInt(site.Version, 10)
	value, err, _ := c.icons.flight.Do(key, func() (any, error) {
		c.icons.mu.Lock()
		cached, ok := c.icons.entries[key]
		c.icons.mu.Unlock()
		if ok && time.Now().Before(cached.expires) {
			return cached.dataURL, cached.err
		}
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		dataURL, err := c.discoverSiteIcon(requestCtx, site)
		if ctx.Err() == nil {
			ttl := time.Hour
			if err != nil {
				ttl = 30 * time.Second
			}
			c.icons.mu.Lock()
			if c.icons.entries == nil || len(c.icons.entries) >= 256 {
				c.icons.entries = make(map[string]siteIconCacheEntry)
			}
			c.icons.entries[key] = siteIconCacheEntry{dataURL: dataURL, err: err, expires: time.Now().Add(ttl)}
			c.icons.mu.Unlock()
		}
		return dataURL, err
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func (c *platformConnector) discoverSiteIcon(ctx context.Context, site Site) (string, error) {
	var branding struct {
		Logo        string `json:"logo"`
		SiteLogo    string `json:"site_logo"`
		SiteFavicon string `json:"site_favicon"`
	}
	path := "/api/v1/settings/public"
	if site.Platform == "newapi" {
		path = "/api/status"
	}
	seen := make(map[string]bool)
	try := func(source string) string {
		source = strings.TrimSpace(source)
		if source == "" || seen[source] {
			return ""
		}
		seen[source] = true
		if strings.HasPrefix(source, "data:image/") {
			header, encoded, ok := strings.Cut(source, ",")
			if !ok || !strings.HasSuffix(header, ";base64") || len(encoded) > base64.StdEncoding.EncodedLen(maxSiteIconBytes) {
				return ""
			}
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return ""
			}
			return siteIconDataURL(raw)
		}
		raw, err := c.publicSiteAsset(ctx, site, source, maxSiteIconBytes)
		if err != nil {
			return ""
		}
		return siteIconDataURL(raw)
	}
	if c.data(ctx, site, Session{UserAgent: defaultSessionUserAgent}, "GET", path, nil, &branding) == nil {
		for _, source := range []string{branding.SiteLogo, branding.Logo, branding.SiteFavicon} {
			if icon := try(source); icon != "" {
				return icon, nil
			}
		}
	}
	if raw, err := c.publicSiteAsset(ctx, site, "/", connectorMaxBody); err == nil {
		for _, source := range declaredSiteIcons(raw) {
			if icon := try(source); icon != "" {
				return icon, nil
			}
		}
	}
	if icon := try("/favicon.ico"); icon != "" {
		return icon, nil
	}
	return "", ErrNotFound
}

func (c *platformConnector) publicSiteAsset(ctx context.Context, site Site, reference string, limit int64) ([]byte, error) {
	base, err := url.Parse(site.BaseURL)
	if err != nil {
		return nil, ErrInvalid
	}
	relative, err := url.Parse(reference)
	if err != nil {
		return nil, ErrInvalid
	}
	target := base.ResolveReference(relative)
	// The shared HTTP transport also validates resolved addresses, including
	// those of an upstream-configured CDN. It keeps the site's selected proxy.
	origin := Site{Name: "icon", Platform: site.Platform, BaseURL: target.Scheme + "://" + target.Host}
	if target.User != nil || validateSite(&origin) != nil {
		return nil, ErrInvalid
	}
	target.Fragment = ""
	req, err := http.NewRequestWithContext(ctx, "GET", target.String(), nil)
	if err != nil || c.factory == nil {
		return nil, ErrInvalid
	}
	req.Header.Set("Accept", "image/*,text/html;q=0.8")
	req.Header.Set("User-Agent", defaultSessionUserAgent)
	client, err := c.factory(ctx, site)
	if err != nil || client == nil {
		return nil, errConnectorRemote
	}
	if concrete, ok := client.(*http.Client); ok {
		copy := *concrete
		copy.Jar = nil
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copy
	}
	response, err := client.Do(req)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil || response == nil || response.Body == nil || response.StatusCode != http.StatusOK {
		return nil, errConnectorRemote
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrUnsupported
	}
	return raw, nil
}

func declaredSiteIcons(raw []byte) []string {
	var icons []string
	tokenizer := html.NewTokenizer(bytes.NewReader(raw))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return icons
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "link" {
				continue
			}
			rel, href := "", ""
			for _, attribute := range token.Attr {
				switch attribute.Key {
				case "rel":
					rel = strings.ToLower(attribute.Val)
				case "href":
					href = attribute.Val
				}
			}
			for _, role := range strings.Fields(rel) {
				if role == "icon" || role == "apple-touch-icon" {
					icons = append(icons, href)
					break
				}
			}
			if len(icons) >= 4 {
				return icons
			}
		}
	}
}

func siteIconDataURL(raw []byte) string {
	if len(raw) == 0 || len(raw) > maxSiteIconBytes {
		return ""
	}
	mime := http.DetectContentType(raw)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/vnd.microsoft.icon", "image/x-icon":
	default:
		decoder := xml.NewDecoder(bytes.NewReader(raw))
		root := false
		for {
			token, err := decoder.Token()
			if err == io.EOF && root {
				break
			}
			if err != nil {
				return ""
			}
			if _, directive := token.(xml.Directive); directive {
				return ""
			}
			if element, ok := token.(xml.StartElement); ok {
				if !root && element.Name.Local != "svg" || element.Name.Local == "script" || element.Name.Local == "foreignObject" {
					return ""
				}
				for _, attribute := range element.Attr {
					if strings.HasPrefix(strings.ToLower(attribute.Name.Local), "on") {
						return ""
					}
				}
				root = true
			}
		}
		mime = "image/svg+xml"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

func (s *Service) SiteIcon(ctx context.Context, siteID int64) (string, error) {
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return "", err
	}
	reader, ok := s.connector.(interface {
		SiteIcon(context.Context, Site) (string, error)
	})
	if !ok {
		return "", ErrUnsupported
	}
	return reader.SiteIcon(ctx, *site)
}
