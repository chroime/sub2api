package service

import (
	"context"
	"database/sql"
	aq "github.com/Wei-Shaw/sub2api/internal/accountquality"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"html"
	"net/url"
	"strings"
)

// The sender deliberately has no recipient fallback. The independent quality
// category and each selected channel are checked before a durable delivery.
type accountQualityNotifier struct {
	email    *EmailService
	astrbot  *governanceBalanceNotifier
	settings SettingRepository
}

func (n accountQualityNotifier) SendAccountQualityNotice(ctx context.Context, target, body string) error {
	if n.settings != nil {
		if base, e := n.settings.GetValue(ctx, SettingKeyFrontendURL); e == nil {
			if parsed, e := url.Parse(strings.TrimSpace(base)); e == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" {
				body = strings.Replace(body, "详情：/admin/", "详情："+strings.TrimRight(base, "/")+"/admin/", 1)
			}
		}
	}
	if strings.HasPrefix(target, "qq:") {
		return n.astrbot.sendAstrBot(ctx, strings.TrimPrefix(target, "qq:"), gov.ChangeNotice{Subject: "账号质量监测", Body: body, AstrBotText: body})
	}
	if n.email == nil {
		return ErrEmailNotConfigured
	}
	return n.email.SendEmail(ctx, target, "账号质量监测通知", "<div style=\"font-family:system-ui,sans-serif;max-width:900px;line-height:1.8\"><h2>账号质量监测</h2><pre style=\"white-space:pre-wrap;font-family:inherit\">"+html.EscapeString(body)+"</pre></div>")
}
func ProvideAccountQualityService(db *sql.DB, probe *AccountTestService, accounts AccountRepository, snapshots *SchedulerSnapshotService, billing *BillingService, email *EmailService, governance *gov.Service, settings SettingRepository) *aq.Service {
	store := aq.NewStore(db)
	store.ResolveModel = func(ctx context.Context, id int64, requested string) (string, error) {
		account, e := accounts.GetByID(ctx, id)
		if e != nil {
			return "", e
		}
		if account.Platform == PlatformOpenAI {
			return resolveOpenAIAccountUpstreamModelForRequest(account, requested, false), nil
		}
		return account.GetMappedModel(requested), nil
	}
	store.Refresh = func(ctx context.Context, id int64) {
		account, e := accounts.GetByID(ctx, id)
		if e == nil && account != nil {
			_ = snapshots.UpdateAccountInCache(ctx, account)
		}
	}
	svc := aq.NewService(store, probe, AccountQualityCapabilities())
	svc.Notifier = accountQualityNotifier{email, &governanceBalanceNotifier{settings: settings, mail: email, runtimeGlobalSettings: governance.GetRuntimeGlobalSettings}, settings}
	svc.Price = func(model string, input, output int64) *float64 {
		if billing == nil {
			return nil
		}
		cost, e := billing.CalculateCost(model, UsageTokens{InputTokens: int(input), OutputTokens: int(output)}, 1)
		if e != nil || cost == nil {
			return nil
		}
		v := cost.TotalCost
		return &v
	}
	svc.Estimate = func(p aq.Policy, prompt string) *float64 {
		if billing == nil {
			return nil
		}
		for _, c := range svc.Capabilities {
			if c.Platform == p.Platform && c.APIMode == p.APIMode && c.OutputLimitMode == "upstream" {
				cost, e := billing.CalculateCost(p.Model, UsageTokens{InputTokens: len([]byte(prompt)) + 1024, OutputTokens: p.MaxOutputTokens}, 1)
				if e == nil && cost != nil {
					v := cost.TotalCost
					return &v
				}
			}
		}
		return nil
	}
	svc.Start()
	return svc
}
