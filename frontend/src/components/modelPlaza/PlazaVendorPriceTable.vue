<template>
  <div
    class="plaza-vendor-price-table overflow-x-auto rounded-2xl border border-gray-100 bg-white shadow-card dark:border-dark-700/50 dark:bg-dark-800/50"
  >
    <table class="w-full min-w-[1080px] border-collapse text-sm tabular-nums">
      <caption class="sr-only">{{ caption }}</caption>
      <thead>
        <!-- 两级表头：本站价与官方价同列序、同格式，便于逐项对比 -->
        <tr class="text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">
          <th scope="col" rowspan="2" class="border-r border-gray-100 py-2.5 pl-5 pr-3 text-left align-middle dark:border-dark-700/60">
            {{ t('modelPlaza.table.model') }}
          </th>
          <th scope="colgroup" colspan="4" class="pz-bg pt-2 text-center">
            <div class="pz-title border-b pb-2 font-semibold">
              {{ t('modelPlaza.priceTable.sitePrice') }}
              <span class="pz-unit ml-1 font-normal normal-case">{{ siteRateNote }}</span>
            </div>
          </th>
          <th scope="colgroup" colspan="4" class="border-l border-gray-100 pt-2 text-center dark:border-dark-700/60">
            <div class="border-b border-gray-200 pb-2 text-gray-400 dark:border-dark-600 dark:text-dark-500">
              {{ t('modelPlaza.priceTable.official') }}
            </div>
          </th>
        </tr>
        <tr
          class="border-b border-gray-200 text-[11px] font-medium uppercase leading-4 tracking-wide text-gray-400 dark:border-dark-700 dark:text-dark-500"
        >
          <th scope="col" class="pz-bg px-3 py-2 text-right font-medium">{{ t('modelPlaza.table.input') }}</th>
          <th scope="col" class="pz-bg px-3 py-2 text-right font-medium">{{ t('modelPlaza.table.output') }}</th>
          <th scope="col" class="pz-bg px-3 py-2 text-right font-medium">{{ t('modelPlaza.priceTable.cacheRead') }}</th>
          <th scope="col" class="pz-bg px-3 py-2 text-right font-medium">{{ t('modelPlaza.priceTable.cacheWrite') }}</th>
          <th scope="col" class="border-l border-gray-100 px-3 py-2 text-right font-medium dark:border-dark-700/60">
            {{ t('modelPlaza.table.input') }}
          </th>
          <th scope="col" class="px-3 py-2 text-right font-medium">{{ t('modelPlaza.table.output') }}</th>
          <th scope="col" class="px-3 py-2 text-right font-medium">{{ t('modelPlaza.priceTable.cacheRead') }}</th>
          <th scope="col" class="px-3 py-2 pr-5 text-right font-medium">{{ t('modelPlaza.priceTable.cacheWrite') }}</th>
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

          <!-- 本站价：已乘生效倍率；未选分组时倍率为 1，等同单价 -->
          <template v-if="billingMode(row.model) === BILLING_MODE_TOKEN">
            <td
              v-for="field in TOKEN_FIELDS"
              :key="`site-${field}`"
              class="pz-cell px-3 py-3 text-right align-middle font-mono"
            >
              <template v-if="isTiered(row.model)">
                <div
                  v-for="(iv, idx) in tokenIntervals(row.model)"
                  :key="idx"
                  class="whitespace-nowrap text-xs leading-5"
                  :title="tierHint(row.model)"
                >
                  <span
                    v-if="field === 'input_price'"
                    class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500"
                  >{{ tierLabel(iv) }}</span>
                  <span class="font-semibold text-gray-900 dark:text-gray-50">
                    {{ sitePrice(row.model, iv[field], row.period) }}
                  </span>
                </div>
              </template>
              <template v-else>
                <div class="font-semibold text-gray-900 dark:text-gray-50">
                  {{ sitePrice(row.model, row.model.pricing?.[field], row.period) }}
                </div>
                <div
                  v-if="field === 'cache_write_price' && row.model.pricing?.cache_write_1h_price != null"
                  class="mt-0.5 whitespace-nowrap text-[11px] font-normal text-gray-500 dark:text-dark-400"
                  :title="t('modelPlaza.priceTable.cacheWrite1hHint')"
                >
                  1h {{ sitePrice(row.model, row.model.pricing.cache_write_1h_price, row.period) }}
                </div>
              </template>
            </td>
          </template>
          <td v-else colspan="4" class="pz-cell px-3 py-3 text-right align-middle">
            <div v-if="requestPrices(row.model).length" class="flex flex-wrap items-center justify-end gap-1.5">
              <span
                v-for="p in requestPrices(row.model)"
                :key="p.label"
                class="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-0.5 font-mono text-xs text-gray-800 dark:bg-dark-700/60 dark:text-gray-200"
              >
                <span v-if="p.label" class="font-sans text-gray-400 dark:text-dark-500">{{ p.label }}</span>
                {{ money(row.model, p.price * periodMultiplier(row.period) * siteRate(row.model), 1)
                }}<span class="font-sans text-gray-400 dark:text-dark-500">{{ perUnitSuffix(row.model) }}</span>
              </span>
            </div>
            <span v-else class="text-gray-400 dark:text-dark-500">-</span>
          </td>

          <!-- 官方价：不乘倍率，列序与本站价一致；时段行无对应官方价 -->
          <template v-if="row.period">
            <td colspan="4" class="border-l border-gray-100 dark:border-dark-700/60"></td>
          </template>
          <template v-else-if="billingMode(row.model) === BILLING_MODE_TOKEN && officialPrices(row.model)">
            <td
              v-for="(field, fieldIdx) in TOKEN_FIELDS"
              :key="`official-${field}`"
              class="px-3 py-3 text-right align-middle font-mono text-xs text-gray-500 dark:text-dark-400"
              :class="[fieldIdx === 0 ? 'border-l border-gray-100 dark:border-dark-700/60' : '',
                       fieldIdx === TOKEN_FIELDS.length - 1 ? 'pr-5' : '']"
            >
              <template v-if="officialIntervals(row.model).length > 1">
                <div
                  v-for="(iv, idx) in officialIntervals(row.model)"
                  :key="idx"
                  class="whitespace-nowrap leading-5"
                  :title="t('modelPlaza.table.tierHint')"
                >
                  <span
                    v-if="field === 'input_price'"
                    class="mr-1 font-sans text-gray-400 dark:text-dark-500"
                  >{{ tierLabel(iv) }}</span>
                  {{ official(row.model, iv[field]) }}
                </div>
              </template>
              <template v-else>
                <div :title="isCny(row.model) ? t('modelPlaza.priceTable.officialCnyHint') : undefined">
                  {{ official(row.model, officialPrices(row.model)![field]) }}
                </div>
                <div
                  v-if="field === 'cache_write_price' && official1hPrice(row.model) != null"
                  class="mt-0.5 whitespace-nowrap text-[11px] text-gray-400 dark:text-dark-500"
                  :title="t('modelPlaza.priceTable.cacheWrite1hHint')"
                >
                  1h {{ official(row.model, official1hPrice(row.model)) }}
                </div>
              </template>
            </td>
          </template>
          <td v-else colspan="4" class="border-l border-gray-100 px-3 py-3 pr-5 text-right align-middle text-xs text-gray-400 dark:border-dark-700/60 dark:text-dark-500">
            -
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
import type { ModelPlazaGroup, PlazaModel, PlazaTimePricingPeriod } from '@/api/modelPlaza'
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

/** 本站价列头的倍率标注；未选分组时倍率为 1。 */
const siteRateNote = computed(() => t('modelPlaza.priceTable.siteRateNote', { rate: groupRate() }))

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

/** 本站生效倍率；图片/视频开启独立倍率时覆盖分组倍率，与计费口径一致。 */
function siteRate(m: PlazaModel): number {
  const g = props.group
  if (!g) return 1
  if (billingMode(m) === BILLING_MODE_IMAGE && g.image_rate_independent) return g.image_rate_multiplier ?? 1
  if (billingMode(m) === BILLING_MODE_VIDEO && g.video_rate_independent) return Math.max(0, g.video_rate_multiplier ?? 1)
  return groupRate()
}

/** 本站价：渠道/分组单价 × 时段倍率 × 本站倍率，与官方价同口径（/ 1M token）。 */
function sitePrice(m: PlazaModel, value: number | null | undefined, period: PlazaTimePricingPeriod | null): string {
  return value == null ? '-' : money(m, value * periodMultiplier(period) * siteRate(m), PER_MILLION)
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

<style scoped>
/* 本站价分区用主色低饱和底色与官方价拉开层次，官方价保持中性 */
.plaza-vendor-price-table {
  --pz-title: theme('colors.primary.700');
  --pz-bg: theme('colors.primary.50 / 60%');
  --pz-bg-hover: theme('colors.primary.50');
}

:global(.dark) .plaza-vendor-price-table {
  --pz-title: theme('colors.primary.300');
  --pz-bg: theme('colors.primary.500 / 8%');
  --pz-bg-hover: theme('colors.primary.500 / 14%');
}

.pz-bg,
.pz-cell {
  background-color: var(--pz-bg);
}

.pz-cell {
  transition: background-color 150ms cubic-bezier(0.4, 0, 0.2, 1);
}

tbody tr:hover .pz-cell {
  background-color: var(--pz-bg-hover);
}

.pz-title {
  color: var(--pz-title);
  border-color: color-mix(in srgb, var(--pz-title) 30%, transparent);
}

.pz-unit {
  color: color-mix(in srgb, var(--pz-title) 62%, transparent);
}
</style>
