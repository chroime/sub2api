package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// PlazaAccountModels 列出分组下启用账号对外暴露的模型，推导规则与 /v1/models 一致：
// 账号 model_mapping → 平台默认模型（有对应账号才回落）→ Grok 语音别名。
// 渠道候选同时按调度路径检查，分组白名单在 service 层按公开模型名过滤。
//
// 与 /v1/models 的差异（广场是橱窗，不是客户端模型目录）：
//   - 取持久启用调度的账号，限流/冷却等临时状态不影响广场展示；
//   - 分组没有任何启用账号时不回落默认模型，避免空分组展示一整套默认清单；
//   - 多协议 API Key 供应商（国产厂商等）没有静态默认清单，只展示账号映射键；
//   - OpenAI 固定清单（Codex manifest）需实时请求上游，这里改用账号映射口径；
//   - composite 分组的路由公开名不带平台归属，不并入。
func (h *GatewayHandler) PlazaAccountModels(ctx context.Context, g *service.Group, candidates []service.PlazaAccountModel) []service.PlazaAccountModel {
	if h == nil || h.gatewayService == nil || g == nil {
		return nil
	}
	accounts, err := h.gatewayService.ListModelPlazaAccounts(ctx, g.ID)
	if err != nil || len(accounts) == 0 {
		return nil
	}
	platforms := []string{g.Platform}
	composite := g.Platform == service.PlatformComposite
	if composite {
		platforms = domain.CompositePrecedencePlatformIDs()
	}

	out := make([]service.PlazaAccountModel, 0)
	for _, platform := range platforms {
		pool := make([]service.Account, 0, len(accounts))
		for _, account := range accounts {
			if account.Status != service.StatusActive || !account.Schedulable {
				continue
			}
			mixed := !composite && (platform == service.PlatformAnthropic || platform == service.PlatformGemini) && account.IsMixedSchedulingEnabled()
			if account.Platform == platform || mixed {
				pool = append(pool, account)
			}
		}
		if len(pool) == 0 {
			continue
		}
		models, _ := service.AvailableModelsFromAccounts(pool, platform)
		// OpenAI 过滤退役模型后的空列表是权威结果，不回落默认清单。
		if len(models) == 0 && (platform != service.PlatformOpenAI || models == nil) {
			if !service.IsMultiProtocolAPIKeyProvider(platform) {
				models = defaultModelIDsForPlatform(platform)
			}
		}
		// 映射键仍需参与候选：治理账号的透传标记不能扩大真实白名单，
		// 混合调度账号也不能因默认清单回落而丢失自定义公开名。
		for _, account := range pool {
			for name := range account.GetModelMapping() {
				if account.Platform == platform || plazaMixedModelBelongsToPlatform(platform, name, account.GetMappedModel(name)) {
					models = mergeModelIDs(models, []string{name})
				}
			}
		}
		for _, candidate := range candidates {
			if candidate.Platform == platform {
				models = mergeModelIDs(models, []string{candidate.Name})
			}
		}
		if platform == service.PlatformGrok && len(models) > 0 {
			models = mergeModelIDs(models, xai.DefaultVoiceModelIDs())
		}
		for _, name := range models {
			// 模式只能为具体渠道/白名单候选提供支持，不能作为展示条目或
			// 绕过某个精确映射的拒绝（例如通配允许但精确目标已退役）。
			if strings.Contains(name, "*") {
				continue
			}
			supported := false
			for i := range pool {
				if h.gatewayService.AccountSupportsModelForListing(ctx, g, &pool[i], name) {
					supported = true
					break
				}
			}
			if supported {
				out = append(out, service.PlazaAccountModel{Name: name, Platform: platform, ChannelRestrictionVerified: true})
			}
		}
	}
	return out
}

func plazaMixedModelBelongsToPlatform(platform, name, mapped string) bool {
	prefix := "claude-"
	if platform == service.PlatformGemini {
		prefix = "gemini-"
	}
	return strings.HasPrefix(name, prefix) || strings.HasPrefix(mapped, prefix)
}
