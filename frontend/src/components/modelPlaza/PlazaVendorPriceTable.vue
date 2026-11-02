<template>
  <div
    class="plaza-vendor-price-table overflow-x-auto rounded-2xl border border-gray-100 bg-white shadow-card dark:border-dark-700/50 dark:bg-dark-800/50"
  >
    <table class="w-full min-w-[920px] border-collapse text-sm tabular-nums">
      <caption class="sr-only">{{ caption }}</caption>
      <thead>
        <tr
          class="border-b border-gray-200 text-xs font-semibold uppercase tracking-wider text-gray-500 dark:border-dark-700 dark:text-dark-400"
        >
          <th scope="col" class="py-3 pl-5 pr-3 text-left">{{ t('modelPlaza.table.model') }}</th>
          <th scope="col" class="px-3 py-3 text-right">{{ t('modelPlaza.table.input') }}</th>
          <th scope="col" class="px-3 py-3 text-right">{{ t('modelPlaza.table.output') }}</th>
          <th scope="col" class="px-3 py-3 text-right">{{ t('modelPlaza.priceTable.cacheRead') }}</th>
          <th scope="col" class="px-3 py-3 text-right">{{ t('modelPlaza.priceTable.cacheWrite') }}</th>
          <th scope="col" class="border-l border-gray-100 px-3 py-3 text-right dark:border-dark-700/60">
            {{ t('modelPlaza.priceTable.official') }}
          </th>
          <th scope="col" class="py-3 pl-3 pr-5 text-left">{{ t('modelPlaza.priceTable.source') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="row in rows"
          :id="row.period ? undefined : rowId(row.model)"
          :key="row.key"
          class="border-b border-gray-100 transition-colors last:border-b-0 dark:border-dark-800"
          :class="[
            highlighted === plazaModelKey(row.model)
              ? 'bg-primary-50/70 dark:bg-primary-500/10'
              : 'hover:bg-gray-50/70 dark:hover:bg-dark-800/50',
            row.period ? 'bg-gray-50/40 dark:bg-dark-900/20' : ''
          ]"
        >
          <th scope="row" class="py-3 pl-5 pr-3 text-left align-middle font-normal">
            <!-- 分时时段行:缩进展示时段与时段倍率 -->
            <div v-if="row.period" class="flex flex-wrap items-center gap-1.5 pl-6" :title="periodHint(row.model)">
              <span
                class="inline-flex items-center whitespace-nowrap rounded-md bg-amber-50 px-1.5 py-0.5 font-mono text-[11px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
              >
                <span v-if="row.model.time_pricing?.weekdays_only" class="mr-1 font-sans">{{ t('modelPlaza.table.timePricingWeekdays') }}</span>
                {{ formatTimeWindow(row.period) }}
              </span>
              <span class="font-mono text-xs text-gray-500 dark:text-dark-400">×{{ row.period.multiplier }}</span>
            </div>
            <div v-else class="flex flex-wrap items-center gap-1.5">
              <ModelIcon :model="row.model.name" size="16px" />
              <span class="font-mono text-[13px] font-medium text-gray-900 dark:text-white">{{ row.model.name }}</span>
              <span
                v-if="billingMode(row.model) !== BILLING_MODE_TOKEN"
                class="rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-700/70 dark:text-dark-300"
              >
                {{ billingMode(row.model) === BILLING_MODE_IMAGE ? t('modelPlaza.table.perImage') : t('modelPlaza.table.perRequest') }}
              </span>
              <span
                v-if="isTiered(row.model)"
                class="rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-700/70 dark:text-dark-300"
                :title="tierHint(row.model)"
              >
                {{ t('modelPlaza.priceTable.tiered') }}
              </span>
              <span
                v-if="row.model.long_context_basis === 'marginal'"
                class="rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-700/70 dark:text-dark-300"
                :title="t('modelPlaza.table.tierHintMarginal')"
              >
                {{ t('modelPlaza.table.marginalBadge') }}
              </span>
              <span
                v-for="[effort, multiplier] in reasoningMultipliers(row.model)"
                :key="effort"
                class="rounded-md bg-amber-50 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                :title="t('modelPlaza.table.reasoningMultiplierHint', { effort, multiplier })"
                :data-reasoning-effort="effort"
              >
                {{ t('modelPlaza.table.reasoningMultiplierBadge', { effort, multiplier }) }}
              </span>
              <span
                v-if="isCny(row.model)"
                class="rounded-md bg-red-50 px-1.5 py-0.5 text-[10px] font-medium text-red-600 dark:bg-red-900/20 dark:text-red-300"
                :title="t('modelPlaza.priceTable.cnyHint')"
              >
                CNY
              </span>
            </div>
          </th>

          <template v-if="billingMode(row.model) === BILLING_MODE_TOKEN">
            <td v-for="field in TOKEN_FIELDS" :key="field" class="px-3 py-3 text-right align-middle">
              <!-- 多档模型逐档展示；档位标签只放在输入列，其余列按行对齐 -->
              <template v-if="isTiered(row.model)">
                <div
                  v-for="(iv, idx) in tokenIntervals(row.model)"
                  :key="idx"
                  class="whitespace-nowrap font-mono text-xs leading-5"
                  :title="tierHint(row.model)"
                >
                  <span
                    v-if="field === 'input_price'"
                    class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500"
                  >{{ tierLabel(iv) }}</span>
                  <span class="font-semibold text-gray-900 dark:text-gray-50">
                    {{ unitPrice(row.model, iv[field], row.period) }}
                  </span>
                  <span
                    v-if="group && iv[field] != null"
                    class="ml-1 text-[11px] font-normal text-primary-600 dark:text-primary-400"
                  >{{ paidTokenPrice(row.model, iv[field], row.period) }}</span>
                </div>
              </template>
              <template v-else>
                <div class="font-mono font-semibold text-gray-900 dark:text-gray-50">
                  {{ unitPrice(row.model, row.model.pricing?.[field], row.period) }}
                </div>
                <!-- 支持 5m/1h 分档的模型，缓存写入列补一行 1h 价 -->
                <div
                  v-if="field === 'cache_write_price' && row.model.pricing?.cache_write_1h_price != null"
                  class="mt-0.5 whitespace-nowrap font-mono text-[11px] text-gray-500 dark:text-dark-400"
                  :title="t('modelPlaza.priceTable.cacheWrite1hHint')"
                >
                  1h {{ unitPrice(row.model, row.model.pricing.cache_write_1h_price, row.period) }}
                </div>
                <div
                  v-if="group && row.model.pricing?.[field] != null"
                  class="mt-0.5 font-mono text-[11px] text-primary-600 dark:text-primary-400"
                >
                  {{ t('modelPlaza.priceTable.paidShort') }} {{ paidTokenPrice(row.model, row.model.pricing[field], row.period) }}
                </div>
              </template>
            </td>
          </template>
          <td v-else colspan="4" class="px-3 py-3 text-right align-middle">
            <div v-if="requestPrices(row.model).length" class="flex flex-wrap items-center justify-end gap-1.5">
              <span
                v-for="p in requestPrices(row.model)"
                :key="p.label"
                class="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-0.5 font-mono text-xs text-gray-800 dark:bg-dark-700/60 dark:text-gray-200"
              >
                <span v-if="p.label" class="font-sans text-gray-400 dark:text-dark-500">{{ p.label }}</span>
                {{ money(row.model, p.price * periodMultiplier(row.period), 1) }}<span class="font-sans text-gray-400 dark:text-dark-500">{{ perUnitSuffix(row.model) }}</span>
                <span v-if="group" class="text-primary-600 dark:text-primary-400">
                  → {{ money(row.model, p.price * periodMultiplier(row.period) * requestRate(row.model), 1) }}
                </span>
              </span>
            </div>
            <span v-else class="text-gray-400 dark:text-dark-500">-</span>
          </td>

          <!-- 官方参考价(美元,不乘倍率);时段行留空 -->
          <td
            class="border-l border-gray-100 px-3 py-3 text-right align-middle font-mono text-xs text-gray-500 dark:border-dark-700/60 dark:text-dark-400"
          >
            <template v-if="!row.period && officialPrices(row.model)">
              <!-- 官方阶梯按目录规则合成，不受分组阶梯开关影响 -->
              <template v-if="officialIntervals(row.model).length > 1">
                <div
                  v-for="(iv, idx) in officialIntervals(row.model)"
                  :key="idx"
                  class="whitespace-nowrap leading-5"
                  :title="t('modelPlaza.table.tierHint')"
                >
                  <span class="mr-1 font-sans text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                  {{ official(row.model, iv.input_price) }}
                  <span class="font-sans text-gray-400 dark:text-dark-500"> / </span>
                  {{ official(row.model, iv.output_price) }}
                </div>
              </template>
              <template v-else>
                <div class="whitespace-nowrap" :title="isCny(row.model) ? t('modelPlaza.priceTable.officialCnyHint') : undefined">
                  <span class="font-sans text-gray-400 dark:text-dark-500">{{ t('modelPlaza.priceTable.inShort') }}</span>
                  {{ official(row.model, officialPrices(row.model)!.input_price) }}
                  <span class="ml-1 font-sans text-gray-400 dark:text-dark-500">{{ t('modelPlaza.priceTable.outShort') }}</span>
                  {{ official(row.model, officialPrices(row.model)!.output_price) }}
                </div>
                <div
                  v-if="officialPrices(row.model)!.cache_read_price != null || officialPrices(row.model)!.cache_write_price != null"
                  class="mt-0.5 whitespace-nowrap text-[11px] text-gray-400 dark:text-dark-500"
                >
                  {{ t('modelPlaza.table.cacheReadShort') }} {{ official(row.model, officialPrices(row.model)!.cache_read_price) }}
                  <span class="ml-1">{{ t('modelPlaza.table.cacheWriteShort') }} {{ official(row.model, officialPrices(row.model)!.cache_write_price) }}</span>
                  <span v-if="official1hPrice(row.model) != null" class="ml-1">
                    (1h {{ official(row.model, official1hPrice(row.model)) }})
                  </span>
                </div>
              </template>
            </template>
            <span v-else-if="!row.period">-</span>
          </td>

          <td class="py-3 pl-3 pr-5 align-middle">
            <template v-if="!row.period">
              <span
                v-if="row.model.pricing_source"
                class="inline-flex items-center whitespace-nowrap rounded-md px-2 py-0.5 text-xs font-medium"
                :class="SOURCE_CLASS[row.model.pricing_source]"
                :title="t(`modelPlaza.source.${row.model.pricing_source}Hint`)"
              >
                {{ t(`modelPlaza.source.${row.model.pricing_source}`) }}
              </span>
              <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ t('modelPlaza.detail.noPricing') }}</span>
            </template>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelIcon from '@/components/common/ModelIcon.vue'
import { formatScaled, resolveIntervalPrices } from '@/utils/pricing'
import { isDomesticModelVendor, modelVendorOf } from '@/utils/modelVendor'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_TOKEN,
  BILLING_MODE_VIDEO,
  REASONING_EFFORT_LEVELS,
  type BillingMode
} from '@/constants/channel'
import type { ModelPlazaGroup, PlazaModel, PlazaPricingSource, PlazaTimePricingPeriod } from '@/api/modelPlaza'
import type { UserPricingInterval } from '@/api/channels'
import { plazaModelKey } from './plazaVendorTree'

const props = defineProps<{
  models: PlazaModel[]
  /** 选中的分组；为空时只展示单价，不计算分组实付价。 */
  group: ModelPlazaGroup | null
  caption: string
  highlighted?: string
  /** 选中分组的高峰窗口描述（含倍率与时区），用于时段行 tooltip 披露。 */
  peakWindow?: string
}>()

const { t } = useI18n()

const PER_MILLION = 1_000_000
const MIN_DECIMALS = 2
const TOKEN_FIELDS = ['input_price', 'output_price', 'cache_read_price', 'cache_write_price'] as const

const SOURCE_CLASS: Record<PlazaPricingSource, string> = {
  group: 'bg-violet-50 text-violet-700 dark:bg-violet-900/20 dark:text-violet-300',
  channel: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300',
  official: 'bg-gray-100 text-gray-600 dark:bg-dark-700/70 dark:text-dark-300'
}

/** 每个模型一行标准价；配置了分时倍率的模型再按时段各加一行。 */
interface PriceRow {
  model: PlazaModel
  period: PlazaTimePricingPeriod | null
  key: string
}

const rows = computed<PriceRow[]>(() =>
  props.models.flatMap((m) => {
    const key = plazaModelKey(m)
    const periods = (m.time_pricing?.periods ?? []).map<PriceRow>((p, idx) => ({ model: m, period: p, key: `${key}:${idx}` }))
    return [{ model: m, period: null, key }, ...periods]
  })
)

function rowId(m: PlazaModel): string {
  return `plaza-model-${plazaModelKey(m).replace(/[^a-zA-Z0-9_-]/g, '_')}`
}

function billingMode(m: PlazaModel): BillingMode {
  return (m.pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
}

/** token 模式的长上下文阶梯；档位的相对倍率在此解析为绝对单价。 */
function tokenIntervals(m: PlazaModel): UserPricingInterval[] {
  if (billingMode(m) !== BILLING_MODE_TOKEN || !m.pricing) return []
  return [...(m.pricing.intervals ?? [])]
    .sort((a, b) => a.min_tokens - b.min_tokens)
    .map((iv) => resolveIntervalPrices(iv, m.pricing!))
}

function isTiered(m: PlazaModel): boolean {
  return tokenIntervals(m).length > 1
}

/**
 * 档位标签：优先后端给出的 tier_label，否则按区间生成——
 * 有上限为「≤上限」，末档为「>下限」。
 */
function tierLabel(iv: UserPricingInterval): string {
  if (iv.tier_label) return iv.tier_label
  return iv.max_tokens == null
    ? `>${formatTokenCount(iv.min_tokens)}`
    : `≤${formatTokenCount(iv.max_tokens)}`
}

function formatTokenCount(n: number): string {
  const trim = (v: number) => String(Math.round(v * 100) / 100)
  if (n >= 1_000_000) return `${trim(n / 1_000_000)}M`
  if (n >= 1_000) return `${trim(n / 1_000)}K`
  return String(n)
}

/** 档位计价说明：整单按所在档，或仅超出阈值的部分按该档。 */
function tierHint(m: PlazaModel): string {
  return m.long_context_basis === 'marginal'
    ? t('modelPlaza.table.tierHintMarginal')
    : t('modelPlaza.table.tierHint')
}

/** 思考等级倍率：仅列出配置了正倍率的等级。 */
function reasoningMultipliers(m: PlazaModel): [string, number][] {
  const configured = m.pricing?.reasoning_effort_multipliers
  if (!configured) return []
  return REASONING_EFFORT_LEVELS.flatMap((effort) => {
    const v = configured[effort]
    return typeof v === 'number' && Number.isFinite(v) && v > 0 ? [[effort, v] as [string, number]] : []
  })
}

/**
 * 国产厂商模型的渠道/分组定价按人民币录入，展示为 ¥；
 * 官方目录价（LiteLLM）始终是美元，来源为官方时仍展示 $。
 */
function isCny(m: PlazaModel): boolean {
  return (m.pricing_source === 'channel' || m.pricing_source === 'group') &&
    isDomesticModelVendor(modelVendorOf(m.name, m.platform))
}

function money(m: PlazaModel, value: number, scale: number): string {
  const s = formatScaled(value, scale, MIN_DECIMALS)
  return isCny(m) ? s.replace(/^\$/, '¥') : s
}

function periodMultiplier(period: PlazaTimePricingPeriod | null): number {
  return period ? period.multiplier : 1
}

/** 分组生效倍率：用户专属倍率优先。 */
function groupRate(): number {
  if (!props.group) return 1
  return props.group.user_rate_multiplier ?? props.group.rate_multiplier
}

/** 图片/视频开启独立倍率时覆盖分组倍率，与计费口径一致。 */
function requestRate(m: PlazaModel): number {
  const g = props.group
  if (!g) return 1
  if (billingMode(m) === BILLING_MODE_IMAGE && g.image_rate_independent) return g.image_rate_multiplier ?? 1
  if (billingMode(m) === BILLING_MODE_VIDEO && g.video_rate_independent) return Math.max(0, g.video_rate_multiplier ?? 1)
  return groupRate()
}

function unitPrice(m: PlazaModel, value: number | null | undefined, period: PlazaTimePricingPeriod | null): string {
  return value == null ? '-' : money(m, value * periodMultiplier(period), PER_MILLION)
}

function paidTokenPrice(m: PlazaModel, value: number | null | undefined, period: PlazaTimePricingPeriod | null): string {
  return value == null ? '-' : money(m, value * periodMultiplier(period) * groupRate(), PER_MILLION)
}

type OfficialTokenPrices = Pick<
  NonNullable<PlazaModel['official_pricing']>,
  'input_price' | 'output_price' | 'cache_read_price' | 'cache_write_price'
>

/**
 * 官方参考价（不乘倍率）：国产模型的渠道/分组价就是按官方人民币定价录入的，
 * 直接作为官方价展示（¥）；其余模型取 LiteLLM 官方美元价。
 */
function officialPrices(m: PlazaModel): OfficialTokenPrices | null {
  if (isCny(m)) return billingMode(m) === BILLING_MODE_TOKEN ? m.pricing : null
  return m.official_pricing
}

function official(m: PlazaModel, value: number | null | undefined): string {
  return value == null ? '-' : money(m, value, PER_MILLION)
}

/** 官方长上下文阶梯；国产模型用渠道价作官方价，不带官方阶梯。 */
function officialIntervals(m: PlazaModel): UserPricingInterval[] {
  if (isCny(m)) return []
  return [...(m.official_pricing?.intervals ?? [])].sort((a, b) => a.min_tokens - b.min_tokens)
}

/** 1h 缓存写入价只在计费区分 5m/1h 时有意义，缺省为 null。 */
function official1hPrice(m: PlazaModel): number | null {
  if (isCny(m)) return m.pricing?.cache_write_1h_price ?? null
  return m.official_pricing?.cache_write_1h_price ?? null
}

function perUnitSuffix(m: PlazaModel): string {
  return billingMode(m) === BILLING_MODE_IMAGE ? t('modelPlaza.table.perUnitImage') : t('modelPlaza.table.perUnitRequest')
}

/** 按次/按图价：有档位时逐档列出，否则取默认按次价。 */
function requestPrices(m: PlazaModel): Array<{ label: string; price: number }> {
  const tiers = (m.pricing?.intervals ?? [])
    .filter((iv) => iv.per_request_price != null)
    .map((iv) => ({ label: iv.tier_label ?? '', price: iv.per_request_price as number }))
  if (tiers.length) return tiers
  const flat = m.pricing?.per_request_price
  return flat == null ? [] : [{ label: '', price: flat }]
}

/** “00:30–08:30”；整分钟的 HH:mm:ss 省略秒。 */
function formatTimeWindow(p: PlazaTimePricingPeriod): string {
  const clock = (v: string) => v.replace(/^(\d{2}:\d{2}):00$/, '$1')
  return `${clock(p.start_time)}–${clock(p.end_time)}`
}

/** 时段行说明：时区、仅工作日，以及与高峰时段叠加的披露。 */
function periodHint(m: PlazaModel): string {
  const key = m.time_pricing?.weekdays_only
    ? 'modelPlaza.table.timePricingRowHintWeekdays'
    : 'modelPlaza.table.timePricingRowHint'
  let hint = t(key, { timezone: m.time_pricing?.timezone })
  if (props.peakWindow && props.group) {
    hint += t('modelPlaza.table.timePricingRowHintPeak', {
      window: props.peakWindow,
      multiplier: props.group.peak_rate_multiplier ?? 1
    })
  }
  return hint
}

defineExpose({ rowId })
</script>
