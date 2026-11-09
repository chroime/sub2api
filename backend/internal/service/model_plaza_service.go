package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// PlazaOfficialPricing 模型广场展示用的官方参考价（USD per token），与计费同源：
// LiteLLM → 内置兜底价卡 → 模型策略。字段为 nil 表示该项缺失（0 视为未配置）。
type PlazaOfficialPricing struct {
	InputPrice        *float64
	OutputPrice       *float64
	CacheWritePrice   *float64 // 5m 缓存写入（= LiteLLM cache_creation）
	CacheWrite1hPrice *float64 // 1h 缓存写入，仅计费会区分 5m/1h 时给出
	CacheReadPrice    *float64
	// Intervals 官方长上下文阶梯（多档时给出），不受分组开关影响；
	// 图片/视频模式下为各尺寸/分辨率档位的单价（PerRequestPrice + TierLabel）。
	Intervals []PricingInterval
	// BillingMode 为空表示 token 计费；图片按张、视频按秒时单价在
	// PerRequestPrice（各档同价）或 Intervals（逐档）里。
	BillingMode     BillingMode
	PerRequestPrice *float64
}

// 模型广场展示定价的来源：分组模型级定价 > 渠道模型定价 > 官方目录价。
const (
	PlazaPricingSourceGroup    = "group"
	PlazaPricingSourceChannel  = "channel"
	PlazaPricingSourceOfficial = "official"
)

// PlazaModel 模型广场中单个模型条目：按实收口径合成的展示定价 + 官方参考价。
type PlazaModel struct {
	Name     string
	Platform string
	Pricing  *ChannelModelPricing
	// PricingSource 展示定价来自哪一层（见 PlazaPricingSource*）；无任何定价为空。
	PricingSource   string
	OfficialPricing *PlazaOfficialPricing
	// LongContextBasis 多档时的计价基准（整单 / 仅超出部分），单档为空。
	LongContextBasis ContextPricingBasis
	// TimePricing 计费会生效的分时倍率时段；无分时为 nil。
	TimePricing *TimePricingSchedule
}

// PlazaGroup 模型广场中以分组为顶层的条目。
//
// 与 AvailableGroupRef 相比多了 Description 与 Models；生产查询只展示分组账号
// 支持且通过分组/渠道限制的模型，渠道配置提供候选名称和定价，不证明模型可用。
type PlazaGroup struct {
	ID                 int64
	SortOrder          int
	Name               string
	Description        string
	I18n               GroupI18n
	Platform           string
	SubscriptionType   string
	RateMultiplier     float64
	PeakRateEnabled    bool
	PeakStart          string
	PeakEnd            string
	PeakRateMultiplier float64
	IsExclusive        bool
	// 图片按次实付倍率：ImageRateIndependent 为 true 时，图片计费模型的实付
	// = 档位价 × ImageRateMultiplier，不乘分组/用户专属倍率（与计费口径一致）。
	ImageRateIndependent bool
	ImageRateMultiplier  float64
	// 视频独立倍率与图片独立倍率分别配置，开启时覆盖分组/用户专属倍率。
	VideoRateIndependent bool
	VideoRateMultiplier  float64
	// LongContextPricingEnabled 分组是否按上下文长度应用阶梯价；关闭时模型展示的是最低档。
	LongContextPricingEnabled bool
	Models                    []PlazaModel
	// CatalogModels 供“全部分组”价格目录展示；Models 仍是具体分组实际可用清单。
	// nil 表示调用方只查询渠道目录，沿用 Models。
	CatalogModels []PlazaModel
}

// PlazaAccountModel 分组账号支持的具体公开模型名。
type PlazaAccountModel struct {
	Name     string
	Platform string
	// ChannelRestrictionVerified confirms that the lister checked the actual
	// route target and account against channel pricing restrictions.
	ChannelRestrictionVerified bool
}

// PlazaAccountModelLister 列出账号模型，并逐项验证渠道/白名单候选。
// 返回值只包含可服务的具体公开名称，不能用通配模式覆盖单个候选的失败结果。
type PlazaAccountModelLister func(ctx context.Context, g *Group, candidates []PlazaAccountModel) []PlazaAccountModel

// ModelPlazaService 聚合模型广场数据。
//
// 模型枚举来自渠道配置与分组账号（见 ListGroupsWithAccountModels）；token 模型的展示单价与阶梯由 BillingService 的阶梯表
// 查询给出（与扣费走同一条解析链与计费函数），图片/按次模型沿用渠道/分组档位价。
type ModelPlazaService struct {
	channelRepo    ChannelRepository
	groupRepo      GroupRepository
	pricingService *PricingService
	billingService *BillingService
	resolver       *ModelPricingResolver
}

// NewModelPlazaService 创建模型广场服务。
func NewModelPlazaService(
	channelRepo ChannelRepository,
	groupRepo GroupRepository,
	pricingService *PricingService,
	billingService *BillingService,
	resolver *ModelPricingResolver,
) *ModelPlazaService {
	return &ModelPlazaService{
		channelRepo:    channelRepo,
		groupRepo:      groupRepo,
		pricingService: pricingService,
		billingService: billingService,
		resolver:       resolver,
	}
}

// ListGroups 返回模型广场数据：每个活跃分组附带其可用模型与定价。
//
// 模型枚举口径与 ListAvailable 一致（Active 渠道、SupportedModels ∪ 全局定价回落、
// 平台隔离），仅把顶层从渠道换成分组：
//   - 渠道按 lower(name) 排序后遍历，保证同名模型去重结果确定；
//   - 同分组同名模型「先见者胜」，仅当已存条目无定价而新条目有定价时升级替换；
//   - token 模型的单价与阶梯按实收口径合成（见 ResolveContextPricingSchedule），
//     图片计费模型的档位价按实收口径合成（见 plazaImageDisplayPricing）；
//   - 每个模型附带官方参考价（查不到为 nil）；
//   - 渠道目录查询只返回 Models 非空的分组；分组按后台 SortOrder 升序（同序按 ID），
//     组内模型按名称排序。
//
// 可见性过滤（专属分组）不在此层做，由 handler 按登录态裁剪。
func (s *ModelPlazaService) ListGroups(ctx context.Context) ([]PlazaGroup, error) {
	return s.ListGroupsWithAccountModels(ctx, nil)
}

// ListGroupsWithAccountModels 以账号支持列表约束渠道与账号侧模型的展示
// （accountModels 为 nil 时等同 ListGroups，仅用于渠道目录查询）：
//   - 渠道里已有且账号支持的模型保持渠道条目与渠道定价不变；
//   - 仅账号侧才有的模型按官方目录价展示，来源标记为 official，目录也查不到时无定价；
//   - 合并后的模型遵循网关渠道定价限制，官方兜底价不能绕过允许列表；
//   - 保留全部活跃分组，空模型列表由页面展示空状态；可见性仍由 handler 裁剪；
//   - 没有绑定渠道但有账号模型的分组同样返回；平台隔离规则与渠道模型一致。
func (s *ModelPlazaService) ListGroupsWithAccountModels(ctx context.Context, accountModels PlazaAccountModelLister) ([]PlazaGroup, error) {
	channels, err := s.channelRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active groups: %w", err)
	}

	groupPlatforms := make(map[int64]string, len(groups))
	for _, group := range groups {
		groupPlatforms[group.ID] = group.Platform
	}
	// 网关按仓库顺序确定生效渠道；快照持有渠道指针，必须与下方展示排序隔离。
	restrictionCache := populateChannelCache(append([]Channel(nil), channels...), groupPlatforms)

	sort.SliceStable(channels, func(i, j int) bool {
		return strings.ToLower(channels[i].Name) < strings.ToLower(channels[j].Name)
	})

	byGroup := make(map[int64]*PlazaGroup, len(groups))
	groupEnt := make(map[int64]*Group, len(groups))
	order := make([]int64, 0, len(groups))
	for i := range groups {
		g := &groups[i]
		byGroup[g.ID] = &PlazaGroup{
			ID:                        g.ID,
			SortOrder:                 g.SortOrder,
			Name:                      g.Name,
			Description:               g.Description,
			I18n:                      g.I18n,
			Platform:                  g.Platform,
			SubscriptionType:          g.SubscriptionType,
			RateMultiplier:            g.RateMultiplier,
			PeakRateEnabled:           g.PeakRateEnabled,
			PeakStart:                 g.PeakStart,
			PeakEnd:                   g.PeakEnd,
			PeakRateMultiplier:        g.PeakRateMultiplier,
			IsExclusive:               g.IsExclusive,
			ImageRateIndependent:      g.ImageRateIndependent,
			ImageRateMultiplier:       g.ImageRateMultiplier,
			VideoRateIndependent:      g.VideoRateIndependent,
			VideoRateMultiplier:       g.VideoRateMultiplier,
			LongContextPricingEnabled: g.LongContextPricingEnabled,
		}
		groupEnt[g.ID] = g
		order = append(order, g.ID)
	}

	type modelKey struct {
		platform string
		name     string
	}
	// modelIdx[groupID][platform+modelName] = index into byGroup[groupID].Models
	modelIdx := make(map[int64]map[modelKey]int, len(groups))
	for i := range channels {
		ch := &channels[i]
		if ch.Status != StatusActive {
			continue
		}
		ch.normalizeBillingModelSource()
		supported := ch.SupportedModels()
		// 回落前记录哪些模型由渠道显式定价；其余由官方目录价补齐。
		sources := make([]string, len(supported))
		for j := range supported {
			if !pricingNeedsFallback(supported[j].Pricing) {
				sources[j] = PlazaPricingSourceChannel
			}
		}
		fillGlobalPricingFallback(s.pricingService, supported)
		for j := range supported {
			if sources[j] == "" && supported[j].Pricing != nil {
				sources[j] = PlazaPricingSourceOfficial
			}
		}

		for _, gid := range ch.GroupIDs {
			pg, ok := byGroup[gid]
			if !ok {
				continue
			}
			idx := modelIdx[gid]
			if idx == nil {
				idx = make(map[modelKey]int, len(supported))
				modelIdx[gid] = idx
			}
			for j := range supported {
				m := supported[j]
				if pg.Platform == PlatformComposite {
					if !isConcreteRequestPlatform(m.Platform) {
						continue
					}
				} else if m.Platform != pg.Platform {
					continue
				}
				key := modelKey{platform: m.Platform, name: m.Name}
				if at, seen := idx[key]; seen {
					// 先见者胜；已存条目无定价、或只有官方回落价而新条目是渠道显式定价时升级。
					existing := &pg.Models[at]
					upgrade := (existing.Pricing == nil && m.Pricing != nil) ||
						(existing.PricingSource != PlazaPricingSourceChannel && sources[j] == PlazaPricingSourceChannel)
					if upgrade {
						existing.Pricing = m.Pricing
						existing.PricingSource = sources[j]
					}
					continue
				}
				idx[key] = len(pg.Models)
				pg.Models = append(pg.Models, PlazaModel{
					Name:          m.Name,
					Platform:      m.Platform,
					Pricing:       m.Pricing,
					PricingSource: sources[j],
				})
			}
		}
	}

	accountSupport := make(map[int64][]PlazaAccountModel, len(groups))
	if accountModels != nil {
		for _, gid := range order {
			pg := byGroup[gid]
			g := groupEnt[gid]
			candidates := make([]PlazaAccountModel, 0, len(pg.Models))
			for _, model := range pg.Models {
				candidates = append(candidates, PlazaAccountModel{
					Name: model.Name, Platform: model.Platform,
				})
			}
			// 白名单可把账号通配模式收敛为具体模型；先给账号验证请求路径，
			// 再按公开名展示，不能只因白名单写了名字便宣称模型可用。
			listedPlatforms := []string{pg.Platform}
			if pg.Platform == PlatformComposite {
				listedPlatforms = domain.ConcretePlatformIDs()
			}
			for _, name := range g.ModelAllowlist.Models {
				if !g.ModelAllowlistEnabled() || strings.Contains(name, wildcardSuffix) {
					continue
				}
				for _, platform := range listedPlatforms {
					candidates = append(candidates, PlazaAccountModel{Name: name, Platform: platform})
				}
			}
			accountSupport[gid] = accountModels(ctx, g, candidates)
			idx := modelIdx[gid]
			if idx == nil {
				idx = make(map[modelKey]int)
				modelIdx[gid] = idx
			}
			for _, am := range accountSupport[gid] {
				name := strings.TrimSpace(am.Name)
				// 账号映射键可能是通配模式，不是可展示的具体模型。
				if name == "" || strings.Contains(name, wildcardSuffix) {
					continue
				}
				if pg.Platform == PlatformComposite {
					if !isConcreteRequestPlatform(am.Platform) {
						continue
					}
				} else if am.Platform != pg.Platform {
					continue
				}
				key := modelKey{platform: am.Platform, name: name}
				if _, seen := idx[key]; seen {
					continue
				}
				idx[key] = len(pg.Models)
				pg.Models = append(pg.Models, s.accountOnlyPlazaModel(name, am.Platform))
			}
		}
	}

	officialMemo := make(map[string]*PlazaOfficialPricing)
	out := make([]PlazaGroup, 0, len(order))
	for _, gid := range order {
		pg := byGroup[gid]
		g := groupEnt[gid]
		allowed := pg.Models[:0]
		for _, model := range pg.Models {
			if !g.ModelAllowlist.Allows(model.Name) {
				continue
			}
			// Composite aliases were checked against their resolved route target
			// by the account lister. A second raw-name pricing check would reject
			// a usable alias whose price belongs to that target model.
			verifiedComposite := false
			if pg.Platform == PlatformComposite {
				for _, support := range accountSupport[gid] {
					if support.Platform == model.Platform && support.Name == model.Name && support.ChannelRestrictionVerified {
						verifiedComposite = true
						break
					}
				}
			}
			if !verifiedComposite && plazaModelRestricted(restrictionCache, gid, model) {
				continue
			}
			allowed = append(allowed, model)
		}
		pg.Models = allowed
		if len(pg.Models) == 0 && accountModels == nil {
			continue
		}
		sort.SliceStable(pg.Models, func(i, j int) bool {
			if pg.Models[i].Name != pg.Models[j].Name {
				return pg.Models[i].Name < pg.Models[j].Name
			}
			return pg.Models[i].Platform < pg.Models[j].Platform
		})
		for j := range pg.Models {
			s.fillDisplayPricing(ctx, &pg.Models[j], g)
			// 无渠道/分组定价、由计费目录（含内置兜底价）给出单价的模型归为官方价。
			if pg.Models[j].PricingSource == "" && pg.Models[j].Pricing != nil {
				pg.Models[j].PricingSource = PlazaPricingSourceOfficial
			}
			pg.Models[j].OfficialPricing = s.lookupOfficialPricing(ctx, pg.Models[j].Name, officialMemo)
		}
		if accountModels != nil {
			// 全部分组保留配置价目目录；具体分组仅返回真实账号支持的模型。
			// 用独立切片筛选，确保不会原地覆盖目录内容及已计算的定价。
			pg.CatalogModels = pg.Models
			available := make([]PlazaModel, 0, len(pg.Models))
			for _, model := range pg.Models {
				if plazaAccountModelSupported(model, accountSupport[gid]) {
					available = append(available, model)
				}
			}
			pg.Models = available
		}
		out = append(out, *pg)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func plazaAccountModelSupported(model PlazaModel, supported []PlazaAccountModel) bool {
	for _, accountModel := range supported {
		if accountModel.Platform == model.Platform && accountModel.Name == model.Name {
			return true
		}
	}
	return false
}

// plazaModelRestricted 与网关调度前检查使用相同的计费模型、平台与定价匹配规则。
func plazaModelRestricted(cache *channelCache, groupID int64, model PlazaModel) bool {
	channel := cache.channelByGroupID[groupID]
	if channel == nil || !channel.IsActive() || !channel.RestrictModels {
		return false
	}
	lk := &channelLookup{cache: cache, channel: channel, platform: model.Platform}
	mapping := resolveMapping(lk, groupID, model.Name)
	billingModel := billingModelForRestriction(mapping.BillingModelSource, model.Name, mapping.MappedModel)
	// upstream 依赖逐账号映射，与网关预检查一致，此处不推断上游模型。
	return billingModel != "" && checkRestricted(lk, groupID, billingModel)
}

// accountOnlyPlazaModel 构造仅账号侧才有的模型条目：没有渠道定价，按官方目录价展示。
func (s *ModelPlazaService) accountOnlyPlazaModel(name, platform string) PlazaModel {
	m := PlazaModel{Name: name, Platform: platform}
	if s.pricingService != nil {
		if lp := s.pricingService.GetModelPricing(name); lp != nil {
			m.Pricing = synthesizePricingFromLiteLLM(lp, nil)
			m.PricingSource = PlazaPricingSourceOfficial
		}
	}
	return m
}

// fillDisplayPricing 把模型的展示定价换成实收口径：
// token 模型取计费阶梯表（单价与档位均由真实计费函数得出），
// 图片/按次模型（或阶梯表不可用时）沿用渠道定价与分组图片档位价。
func (s *ModelPlazaService) fillDisplayPricing(ctx context.Context, m *PlazaModel, g *Group) {
	if groupPricing := matchGroupModelPricing(g, m.Name); groupPricing != nil {
		m.Pricing = groupPricing
		m.PricingSource = PlazaPricingSourceGroup
		// 分组模型级固定价就是实收价，不能再被分组图片兜底档位覆盖。
		// 否则模型广场会显示 image_price_1k/2k/4k，而实际按 model_pricing 扣费。
		if groupPricing.BillingMode == BillingModeImage || groupPricing.BillingMode == BillingModePerRequest {
			return
		}
	}
	if s.billingService != nil && s.resolver != nil {
		sched, err := s.billingService.ResolveContextPricingSchedule(ctx, s.resolver, ContextPricingScheduleInput{
			Model:    m.Name,
			Group:    g,
			Platform: m.Platform,
		})
		if err == nil && sched != nil && len(sched.Tiers) > 0 {
			m.Pricing = plazaPricingFromSchedule(m.Pricing, sched)
			if len(sched.Tiers) > 1 {
				m.LongContextBasis = sched.Basis
			}
			m.TimePricing = sched.TimePricing
			return
		}
	}
	m.Pricing = plazaImageDisplayPricing(m.Pricing, g)
	if m.Pricing == nil {
		if pricing, fromGroup := s.grokMediaDisplayPricing(m.Name, g); pricing != nil {
			m.Pricing = pricing
			if fromGroup {
				m.PricingSource = PlazaPricingSourceGroup
			}
		}
	}
}

// plazaMediaTier 图片尺寸或视频分辨率的一个档位单价。
type plazaMediaTier struct {
	label string
	price float64
}

// grokImagineVideoResolutions 列出视频模型可选的分辨率：1.5 系列到 1080p，其余到 720p。
func grokImagineVideoResolutions(model string) []string {
	if CanonicalGrokImagineVideoPriceFamily(model) == VideoPriceFamilyGrokImagineVideo15 {
		return []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P}
	}
	return []string{VideoBillingResolution480P, VideoBillingResolution720P}
}

// grokMediaDisplayPricing 为没有渠道/分组模型级定价的 Grok Imagine 图片与视频模型合成展示定价。
// 档位单价直接取计费函数的结果（分组图片/视频档位价 > 内置的 xAI 官方价），图片每张、视频每秒；
// 不是 Grok Imagine 媒体模型或没有计费服务时返回 nil。fromGroup 表示有档位取自分组配置。
func (s *ModelPlazaService) grokMediaDisplayPricing(model string, g *Group) (*ChannelModelPricing, bool) {
	if s.billingService == nil {
		return nil, false
	}
	fromGroup := false
	if _, ok := getDefaultGrokImagineImagePrice(model, ImageBillingSize1K); ok {
		// 与 imagePriceConfigFromAPIKey 同口径。
		var cfg *ImagePriceConfig
		if g != nil {
			cfg = &ImagePriceConfig{Price1K: g.ImagePrice1K, Price2K: g.ImagePrice2K, Price4K: g.ImagePrice4K}
		}
		tiers := make([]plazaMediaTier, 0, 2)
		for _, size := range []string{ImageBillingSize1K, ImageBillingSize2K} {
			tiers = append(tiers, plazaMediaTier{label: size, price: s.billingService.getImageUnitPrice(model, size, cfg)})
			fromGroup = fromGroup || (g != nil && g.GetImagePrice(size) != nil)
		}
		return plazaMediaPricing(BillingModeImage, tiers), fromGroup
	}
	if _, ok := getDefaultGrokImagineVideoPrice(model, VideoBillingResolution480P); ok {
		// 与 videoPriceConfigFromAPIKey 同口径。
		var cfg *VideoPriceConfig
		if g != nil {
			cfg = &VideoPriceConfig{Price480P: g.VideoPrice480P, Price720P: g.VideoPrice720P, Price1080P: g.VideoPrice1080P, ModelPrices: g.VideoModelPrices}
		}
		resolutions := grokImagineVideoResolutions(model)
		tiers := make([]plazaMediaTier, 0, len(resolutions))
		for _, res := range resolutions {
			tiers = append(tiers, plazaMediaTier{label: strings.ToUpper(res), price: s.billingService.getVideoUnitPrice(model, res, cfg)})
			fromGroup = fromGroup || g.GetVideoPriceForModel(model, res) != nil
		}
		return plazaMediaPricing(BillingModeVideo, tiers), fromGroup
	}
	return nil, false
}

// grokMediaOfficialPricing 返回 Grok Imagine 图片/视频模型的 xAI 官方单价：计费内置价，不含分组覆盖。
func grokMediaOfficialPricing(model string) *PlazaOfficialPricing {
	var mode BillingMode
	var tiers []plazaMediaTier
	if _, ok := getDefaultGrokImagineImagePrice(model, ImageBillingSize1K); ok {
		mode = BillingModeImage
		for _, size := range []string{ImageBillingSize1K, ImageBillingSize2K} {
			price, _ := getDefaultGrokImagineImagePrice(model, size)
			tiers = append(tiers, plazaMediaTier{label: size, price: price})
		}
	} else if _, ok := getDefaultGrokImagineVideoPrice(model, VideoBillingResolution480P); ok {
		mode = BillingModeVideo
		for _, res := range grokImagineVideoResolutions(model) {
			price, _ := getDefaultGrokImagineVideoPrice(model, res)
			tiers = append(tiers, plazaMediaTier{label: strings.ToUpper(res), price: price})
		}
	} else {
		return nil
	}
	p := plazaMediaPricing(mode, tiers)
	return &PlazaOfficialPricing{BillingMode: mode, PerRequestPrice: p.PerRequestPrice, Intervals: p.Intervals}
}

// plazaMediaPricing 把媒体档位价压成展示定价：各档同价时只给一个单价，否则逐档给出。
func plazaMediaPricing(mode BillingMode, tiers []plazaMediaTier) *ChannelModelPricing {
	out := &ChannelModelPricing{BillingMode: mode}
	uniform := true
	for _, t := range tiers[1:] {
		if t.price != tiers[0].price {
			uniform = false
			break
		}
	}
	if uniform {
		price := tiers[0].price
		out.PerRequestPrice = &price
		return out
	}
	out.Intervals = make([]PricingInterval, 0, len(tiers))
	for i, t := range tiers {
		price := t.price
		out.Intervals = append(out.Intervals, PricingInterval{TierLabel: t.label, PerRequestPrice: &price, SortOrder: i})
	}
	return out
}

// plazaPricingFromSchedule 把阶梯表压成展示用的 ChannelModelPricing：
// 平价取首档单价，多档时 Intervals 逐档给出绝对单价；图片/按次字段沿用原始定价。
func plazaPricingFromSchedule(raw *ChannelModelPricing, sched *ContextPricingSchedule) *ChannelModelPricing {
	out := &ChannelModelPricing{BillingMode: BillingModeToken}
	if raw != nil {
		out.ImageInputPrice = raw.ImageInputPrice
		out.ImageOutputPrice = raw.ImageOutputPrice
		out.PerRequestPrice = raw.PerRequestPrice
		out.ReasoningEffortMultipliers = reasoningEffortMultipliersFromPricing(raw)
	}
	first := sched.Tiers[0]
	out.InputPrice = first.Input
	out.OutputPrice = first.Output
	out.CacheWritePrice = first.CacheWrite
	out.CacheWrite1hPrice = first.CacheWrite1h
	out.CacheReadPrice = first.CacheRead
	if len(sched.Tiers) > 1 {
		out.Intervals = plazaIntervalsFromTiers(sched.Tiers)
	}
	return out
}

func plazaIntervalsFromTiers(tiers []ContextPricingTier) []PricingInterval {
	intervals := make([]PricingInterval, 0, len(tiers))
	for i, t := range tiers {
		intervals = append(intervals, PricingInterval{
			MinTokens:         t.MinTokens,
			MaxTokens:         t.MaxTokens,
			TierLabel:         t.Label,
			InputPrice:        t.Input,
			OutputPrice:       t.Output,
			CacheWritePrice:   t.CacheWrite,
			CacheWrite1hPrice: t.CacheWrite1h,
			CacheReadPrice:    t.CacheRead,
			SortOrder:         i,
		})
	}
	return intervals
}

// plazaImageDisplayPricing 为图片计费模型合成展示定价，使档位价与实收口径一致：
// 每档（1K/2K/4K）单价 = 分组图片价 > 渠道同档位价 > 渠道默认按次价，无价的档不展示。
// 分组未配任何图片价、或定价非图片模式时原样返回。返回克隆，不修改入参
// （渠道定价指针指向缓存共享数据）。
func plazaImageDisplayPricing(p *ChannelModelPricing, g *Group) *ChannelModelPricing {
	if p == nil || g == nil || p.BillingMode != BillingModeImage {
		return p
	}
	if g.ImagePrice1K == nil && g.ImagePrice2K == nil && g.ImagePrice4K == nil {
		return p
	}
	channelTierPrice := func(label string) *float64 {
		for i := range p.Intervals {
			if p.Intervals[i].TierLabel == label && p.Intervals[i].PerRequestPrice != nil {
				return p.Intervals[i].PerRequestPrice
			}
		}
		return p.PerRequestPrice
	}
	tiers := []struct {
		label      string
		groupPrice *float64
	}{
		{"1K", g.ImagePrice1K},
		{"2K", g.ImagePrice2K},
		{"4K", g.ImagePrice4K},
	}
	clone := *p
	clone.Intervals = make([]PricingInterval, 0, len(tiers))
	for i, t := range tiers {
		price := t.groupPrice
		if price == nil {
			price = channelTierPrice(t.label)
		}
		if price == nil {
			continue
		}
		v := *price
		clone.Intervals = append(clone.Intervals, PricingInterval{
			TierLabel:       t.label,
			PerRequestPrice: &v,
			SortOrder:       i,
		})
	}
	return &clone
}

// lookupOfficialPricing 查询模型的官方参考价（与计费同源：LiteLLM → 内置兜底 → 模型策略），
// 带 memo 避免同名模型重复解析。官方阶梯按无分组、无渠道的口径查阶梯表。
// billingService 为 nil（测试场景）或查不到时返回 nil。
func (s *ModelPlazaService) lookupOfficialPricing(ctx context.Context, modelName string, memo map[string]*PlazaOfficialPricing) *PlazaOfficialPricing {
	if s.billingService == nil {
		return nil
	}
	if cached, ok := memo[modelName]; ok {
		return cached
	}
	var result *PlazaOfficialPricing
	if mp, err := s.billingService.GetModelPricing(modelName); err == nil && mp != nil {
		result = &PlazaOfficialPricing{
			InputPrice:      nonZeroPtr(mp.InputPricePerToken),
			OutputPrice:     nonZeroPtr(mp.OutputPricePerToken),
			CacheWritePrice: nonZeroPtr(mp.CacheCreationPricePerToken),
			CacheReadPrice:  nonZeroPtr(mp.CacheReadPricePerToken),
		}
		// 计费只在支持 5m/1h 分档时使用 1h 价，其余情况 1h 价对用户无意义。
		if mp.SupportsCacheBreakdown {
			result.CacheWrite1hPrice = nonZeroPtr(mp.CacheCreation1hPrice)
		}
		if s.resolver != nil {
			sched, schedErr := s.billingService.ResolveContextPricingSchedule(ctx, s.resolver, ContextPricingScheduleInput{Model: modelName})
			if schedErr == nil && sched != nil && len(sched.Tiers) > 1 {
				result.Intervals = plazaIntervalsFromTiers(sched.Tiers)
			}
		}
		if result.InputPrice == nil && result.OutputPrice == nil && result.CacheWritePrice == nil &&
			result.CacheWrite1hPrice == nil && result.CacheReadPrice == nil && len(result.Intervals) == 0 {
			result = nil
		}
	}
	if result == nil {
		// LiteLLM 目录不收录 Grok Imagine 的按张/按秒价，取计费内置的 xAI 官方价。
		result = grokMediaOfficialPricing(modelName)
	}
	memo[modelName] = result
	return result
}
