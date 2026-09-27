package service

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/upstreambrowser"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

func governanceProxyURL(ctx context.Context, proxies ProxyRepository, site gov.Site) (string, error) {
	if site.ProxyID == nil {
		return "", nil
	}
	if *site.ProxyID <= 0 || proxies == nil {
		return "", gov.ErrInvalid
	}
	p, err := proxies.GetByID(ctx, *site.ProxyID)
	if err != nil || p == nil || !p.IsActive() || p.IsExpired(time.Now()) || strings.TrimSpace(p.Host) == "" || p.Port < 1 || p.Port > 65535 {
		return "", gov.ErrInvalid
	}
	switch p.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", gov.ErrInvalid
	}
	return p.URL(), nil
}

func configureGovernanceBrowser(svc *gov.Service, proxies ProxyRepository) {
	driver := upstreambrowser.New(upstreambrowser.Options{
		NodePath:       os.Getenv("GOVERNANCE_BROWSER_NODE"),
		ScriptPath:     os.Getenv("GOVERNANCE_BROWSER_SCRIPT"),
		ExecutablePath: os.Getenv("GOVERNANCE_BROWSER_EXECUTABLE"),
		TempDir:        os.Getenv("GOVERNANCE_BROWSER_TEMP_DIR"),
	})
	svc.SetBrowserAuthorizer(gov.NewBrowserAuthorizer(svc, driver, func(ctx context.Context, site gov.Site) (string, error) {
		return governanceProxyURL(ctx, proxies, site)
	}))
}
