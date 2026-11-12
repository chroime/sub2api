import type { ModelPlazaGroup, PlazaModel, PlazaPricingSource } from '@/api/modelPlaza'
import { modelVendorLabel, modelVendorOf, OTHER_MODEL_VENDOR } from '@/utils/modelVendor'

/** 厂商树节点：同一模型厂商下的模型（已按名称排好序）。 */
export interface PlazaVendorNode {
  key: string
  label: string
  /** 供 ModelIcon 识别厂商图标的代表模型名。 */
  sampleModel: string
  models: PlazaModel[]
}

export function plazaModelKey(m: Pick<PlazaModel, 'platform' | 'name'>): string {
  return `${m.platform}:${m.name}`
}

/**
 * 「全部分组」视图下同名模型只展示一条单价，取价顺序与单个分组一致：
 * 分组模型级定价 > 渠道显式定价 > 官方目录价（渠道未配价时的回落）。
 */
const ALL_GROUPS_SOURCE_RANK: Record<PlazaPricingSource, number> = {
  group: 0,
  channel: 1,
  official: 2
}

function sourceRank(m: PlazaModel): number {
  return m.pricing_source ? ALL_GROUPS_SOURCE_RANK[m.pricing_source] : 3
}

/** 具体分组只展示实际可用模型；「全部」按模型名合并不同平台的目录项，兼容旧响应。 */
export function collectPlazaModels(groups: ModelPlazaGroup[], groupId: number | 'all'): PlazaModel[] {
  if (groupId !== 'all') {
    return groups.find((g) => g.id === groupId)?.models ?? []
  }
  const byKey = new Map<string, PlazaModel>()
  for (const g of groups) {
    for (const m of g.catalog_models ?? g.models) {
      const key = m.name
      const current = byKey.get(key)
      if (!current || sourceRank(m) < sourceRank(current)) {
        byKey.set(key, m)
      }
    }
  }
  return [...byKey.values()]
}

/** 版本号按数字比较，较新的版本排在前面（gpt-5.10 先于 gpt-5.9）。 */
function compareModelNames(a: PlazaModel, b: PlazaModel): number {
  return b.name.localeCompare(a.name, undefined, { numeric: true }) || a.platform.localeCompare(b.platform)
}

/**
 * 按模型厂商分组并按搜索词过滤。厂商按模型数降序、同数按名称，「其他」恒排最后。
 */
export function buildVendorTree(models: PlazaModel[], search: string, otherLabel: string): PlazaVendorNode[] {
  const q = search.trim().toLowerCase()
  const byVendor = new Map<string, PlazaModel[]>()
  for (const m of models) {
    if (q && !m.name.toLowerCase().includes(q)) continue
    const key = modelVendorOf(m.name, m.platform)
    const list = byVendor.get(key)
    if (list) list.push(m)
    else byVendor.set(key, [m])
  }
  const nodes = [...byVendor.entries()].map<PlazaVendorNode>(([key, list]) => {
    const sorted = [...list].sort(compareModelNames)
    return {
      key,
      label: key === OTHER_MODEL_VENDOR ? otherLabel : modelVendorLabel(key),
      sampleModel: sorted[0].name,
      models: sorted
    }
  })
  return nodes.sort((a, b) => {
    const ao = a.key === OTHER_MODEL_VENDOR
    const bo = b.key === OTHER_MODEL_VENDOR
    if (ao !== bo) return ao ? 1 : -1
    return b.models.length - a.models.length || a.label.localeCompare(b.label)
  })
}
