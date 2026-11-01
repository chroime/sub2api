package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// PlazaAccountModels 列出分组下启用账号对外暴露的模型，推导规则与 /v1/models 一致：
// 账号 model_mapping → 平台默认模型（有账号才回落）→ Grok 语音别名 → 分组模型白名单。
//
// 与 /v1/models 的差异（广场是橱窗，不是客户端模型目录）：
//   - 取「启用」账号而非「可调度」账号：限流/冷却等临时状态不影响广场展示；
//   - 分组没有任何启用账号时不回落默认模型，避免空分组展示一整套默认清单；
//   - 多协议 API Key 供应商（国产厂商等）没有静态默认清单，只展示账号映射键；
//   - OpenAI 固定清单（Codex manifest）需实时请求上游，这里改用账号映射口径；
//   - composite 分组的路由公开名不带平台归属，不并入。
func (h *GatewayHandler) PlazaAccountModels(ctx context.Context, g *service.Group) []service.PlazaAccountModel {
	if h == nil || h.gatewayService == nil || g == nil {
		return nil
	}
	accounts, err := h.gatewayService.ListActiveAccountsByGroup(ctx, g.ID)
	if err != nil || len(accounts) == 0 {
		return nil
	}
	accountPlatforms := make(map[string]struct{}, len(accounts))
	for i := range accounts {
		if p := strings.TrimSpace(accounts[i].Platform); p != "" {
			accountPlatforms[p] = struct{}{}
		}
	}

	platforms := []string{g.Platform}
	composite := g.Platform == service.PlatformComposite
	if composite {
		platforms = domain.CompositePrecedencePlatformIDs()
	}

	out := make([]service.PlazaAccountModel, 0)
	for _, platform := range platforms {
		_, hasAccounts := accountPlatforms[platform]
		if composite && !hasAccounts {
			continue
		}
		models, _ := service.AvailableModelsFromAccounts(accounts, platform)
		// OpenAI 过滤退役模型后的空列表是权威结果，不回落默认清单。
		if len(models) == 0 && (platform != service.PlatformOpenAI || models == nil) {
			if !service.IsMultiProtocolAPIKeyProvider(platform) {
				models = defaultModelIDsForPlatform(platform)
			}
		}
		if platform == service.PlatformGrok && len(models) > 0 {
			models = mergeModelIDs(models, xai.DefaultVoiceModelIDs())
		}
		if g.ModelAllowlistEnabled() {
			models = g.ModelAllowlist.FilterForListing(models)
		}
		for _, name := range models {
			out = append(out, service.PlazaAccountModel{Name: name, Platform: platform})
		}
	}
	return out
}
