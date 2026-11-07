<template>
  <div
    class="plaza-vendor-price-table relative max-w-full overflow-x-auto rounded-2xl border border-gray-100 bg-white shadow-card focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:border-dark-700/50 dark:bg-dark-800/50"
    role="region"
    tabindex="0"
    :aria-label="caption"
  >
    <div v-if="group" class="pz-comparison-heading flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-dark-700">
      <div class="min-w-0">
        <p class="pz-title text-base font-semibold">{{ t('modelPlaza.comparison.title') }}</p>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('modelPlaza.comparison.subtitle') }}</p>
      </div>
      <span class="pz-rate-badge shrink-0 rounded-lg px-3 py-1.5 text-sm font-semibold" :title="t('modelPlaza.comparison.rateDetails')">{{ siteRateNote }}</span>
    </div>
    <table class="w-full border-collapse text-sm tabular-nums" :class="group ? 'min-w-[780px]' : 'min-w-[560px]'">
      <caption class="sr-only">{{ caption }}</caption>
      <thead>
        <!-- 全部分组只看官方价；选中分组后按相同列序对照本站价。 -->
        <tr class="text-sm font-semibold tracking-wide text-gray-500 dark:text-dark-400">
          <th
            scope="col"
            rowspan="2"
            class="pz-sticky pz-sticky-head py-2.5 pl-5 pr-3 text-left align-middle"
            :class="group ? 'w-36 sm:w-auto' : 'w-36 sm:w-[36%]'"
          >
            {{ t('modelPlaza.table.model') }}
          </th>
          <th v-if="group" scope="colgroup" colspan="4" class="pz-bg pt-2 text-center">
            <div class="pz-title border-b pb-2 font-semibold">
              {{ t('modelPlaza.priceTable.sitePrice') }}
              <span class="pz-unit ml-1 font-normal normal-case">{{ t('modelPlaza.comparison.standardPeriod') }}</span>
            </div>
          </th>
          <th v-else scope="colgroup" colspan="4" class="border-l border-gray-100 pt-2 text-center dark:border-dark-700/60">
            <div class="border-b border-gray-200 pb-2 text-gray-400 dark:border-dark-600 dark:text-dark-500">
              {{ t('modelPlaza.priceTable.official') }}
            </div>
          </th>
        </tr>
        <tr
          class="border-b border-gray-200 text-sm font-medium leading-5 text-gray-400 dark:border-dark-700 dark:text-dark-500"
        >
          <th scope="col" class="border-l border-gray-100 px-3 py-2 text-right font-medium dark:border-dark-700/60" :class="{ 'pz-bg': group }">
            {{ t('modelPlaza.table.input') }}
          </th>
          <th scope="col" class="px-3 py-2 text-right font-medium" :class="{ 'pz-bg': group }">{{ t('modelPlaza.table.output') }}</th>
          <th scope="col" class="px-3 py-2 text-right font-medium" :class="{ 'pz-bg': group }">{{ t('modelPlaza.priceTable.cacheRead') }}</th>
          <th scope="col" class="px-3 py-2 pr-5 text-right font-medium" :class="{ 'pz-bg': group }">{{ t('modelPlaza.priceTable.cacheWrite') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="m in models"
          :id="rowId(m)"
          :key="plazaModelKey(m)"
          class="border-b border-gray-100 transition-colors last:border-b-0 dark:border-dark-800"
          :class="highlighted === plazaModelKey(m)
            ? 'pz-row-highlight bg-primary-50/70 dark:bg-primary-500/10'
            : 'hover:bg-gray-50/70 dark:hover:bg-dark-800/50'"
        >
          <th scope="row" class="pz-sticky py-3 pl-5 pr-3 text-left align-middle font-normal">
            <div class="flex flex-wrap items-center gap-1.5">
              <ModelIcon :model="m.name" size="16px" />
              <span class="break-all font-mono text-[15px] font-semibold text-gray-900 dark:text-white">{{ m.name }}</span>
              <span
                v-if="billingMode(m) !== BILLING_MODE_TOKEN"
                class="rounded-md bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-500 dark:bg-dark-700/70 dark:text-dark-300"
              >
                {{ unitBadge(billingMode(m)) }}
              </span>
              <span
                v-if="isTiered(m)"
                class="rounded-md bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-500 dark:bg-dark-700/70 dark:text-dark-300"
                :title="tierHint(m)"
              >
                {{ t('modelPlaza.priceTable.tiered') }}
              </span>
              <span
                v-if="group && m.long_context_basis === 'marginal'"
                class="rounded-md bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-500 dark:bg-dark-700/70 dark:text-dark-300"
                :title="t('modelPlaza.table.tierHintMarginal')"
              >
                {{ t('modelPlaza.table.marginalBadge') }}
              </span>
              <span v-if="independentRate(m) != null" class="pz-rate-badge rounded-md px-1.5 py-0.5 text-xs font-medium">
                {{ t('modelPlaza.comparison.independentRate', { rate: independentRate(m) }) }}
              </span>
              <span
                v-for="[effort, multiplier] in reasoningMultipliers(m)"
                :key="effort"
                class="rounded-md bg-amber-50 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                :title="t('modelPlaza.table.reasoningMultiplierHint', { effort, multiplier })"
                :data-reasoning-effort="effort"
              >
                {{ t('modelPlaza.table.reasoningMultiplierBadge', { effort, multiplier }) }}
              </span>
              <span
                v-if="isCny(m)"
                class="rounded-md bg-red-50 px-1.5 py-0.5 text-xs font-medium text-red-600 dark:bg-red-900/20 dark:text-red-300"
                :title="t('modelPlaza.priceTable.cnyHint')"
              >
                CNY
              </span>
            </div>
            <!-- 分时倍率：时段内发起的请求整单乘倍率，只影响本站价 -->
            <div
              v-if="timePeriods(m).length"
              class="mt-1.5 flex flex-wrap items-center gap-1 text-xs text-amber-700 dark:text-amber-300"
              :title="periodHint(m)"
              data-time-pricing
            >
              <span v-if="m.time_pricing?.weekdays_only">{{ t('modelPlaza.table.timePricingWeekdays') }}</span>
              <span
                v-for="(period, idx) in timePeriods(m)"
                :key="idx"
                class="inline-flex items-center gap-1 whitespace-nowrap rounded-md bg-amber-50 px-1.5 py-0.5 font-mono font-medium dark:bg-amber-900/20"
              >
                {{ formatTimeWindow(period) }}<span class="font-sans">×{{ period.multiplier }}</span>
              </span>
            </div>
          </th>

          <!-- 本站与官方都没有价格：合并成一格说明，避免整行破折号 -->
          <td
            v-if="isUnpriced(m)"
            colspan="4"
            class="px-2.5 py-3 pr-5 text-center align-middle text-sm text-gray-400 dark:text-dark-500"
            data-unpriced
          >
            {{ t('modelPlaza.priceTable.unpriced') }}
          </td>
          <template v-else>
            <!-- 每个计费项就地对照参考价与折后价，避免跨八列找差价。 -->
            <template v-if="group">
              <template v-if="billingMode(m) === BILLING_MODE_TOKEN">
                <td v-for="field in TOKEN_FIELDS" :key="field" class="pz-cell px-3 py-4 text-right align-top">
                  <div class="space-y-5">
                    <PlazaPriceComparisonCell
                      v-for="price in getTokenComparisons(m, group, field)"
                      :key="price.key"
                      :price="price"
                    />
                  </div>
                </td>
              </template>
              <td v-else colspan="4" class="pz-cell px-4 py-4 align-top">
                <div v-if="getUnitComparisons(m, group).length" class="flex flex-wrap justify-end gap-x-8 gap-y-4">
                  <PlazaPriceComparisonCell
                    v-for="price in getUnitComparisons(m, group)"
                    :key="price.key"
                    :price="price"
                    :unit="unitSuffix(price.billingMode)"
                    class="min-w-28 text-right"
                  />
                </div>
                <p v-else class="text-right text-xs text-gray-400 dark:text-dark-400">—</p>
              </td>
            </template>
            <template v-else>
              <!-- 官方价：不乘倍率，列序与本站价一致 -->
              <template v-if="officialTokenRow(m)">
                <td
                  v-for="(field, fieldIdx) in TOKEN_FIELDS"
                  :key="`official-${field}`"
                  class="px-2.5 py-3 text-right align-middle font-mono text-base text-gray-500 dark:text-dark-400"
                  :class="[fieldIdx === 0 ? 'border-l border-gray-100 dark:border-dark-700/60' : '',
                           fieldIdx === TOKEN_FIELDS.length - 1 ? 'pr-5' : '']"
                >
                  <template v-if="officialIntervals(m).length > 1">
                    <div
                      v-for="(iv, idx) in officialIntervals(m)"
                      :key="idx"
                      class="whitespace-nowrap leading-5"
                      :title="t('modelPlaza.table.tierHint')"
                    >
                      <span
                        v-if="field === 'input_price'"
                        class="mr-1 font-sans text-gray-400 dark:text-dark-500"
                      >{{ tierLabel(iv) }}</span>
                      <span>{{ official(m, iv[field]) }}</span>
                    </div>
                  </template>
                  <template v-else>
                    <div :title="isCny(m) ? t('modelPlaza.priceTable.officialCnyHint') : undefined">
                      {{ official(m, officialPrices(m)![field]) }}
                    </div>
                    <div
                      v-if="field === 'cache_write_price' && official1hPrice(m) != null"
                      class="mt-0.5 whitespace-nowrap text-sm text-gray-400 dark:text-dark-500"
                      :title="t('modelPlaza.priceTable.cacheWrite1hHint')"
                    >
                      1h {{ official(m, official1hPrice(m)) }}
                    </div>
                  </template>
                </td>
              </template>
              <td
                v-else-if="officialUnitPrices(m).length"
                colspan="4"
                class="border-l border-gray-100 px-2.5 py-3 pr-5 text-right align-middle dark:border-dark-700/60"
              >
                <div class="flex flex-wrap items-center justify-end gap-1.5">
                  <span
                    v-for="p in officialUnitPrices(m)"
                    :key="p.label"
                    class="inline-flex items-center gap-1 rounded-md px-2 py-0.5 font-mono text-base text-gray-500 ring-1 ring-inset ring-gray-200 dark:text-dark-400 dark:ring-dark-700"
                  >
                    <span v-if="p.label" class="font-sans text-gray-400 dark:text-dark-500">{{ p.label }}</span>
                    {{ money(m, p.price, 1)
                    }}<span class="font-sans text-gray-400 dark:text-dark-500">{{ unitSuffix(officialMode(m)) }}</span>
                  </span>
                </div>
              </td>
              <td v-else colspan="4" class="border-l border-gray-100 px-2.5 py-3 pr-5 text-right align-middle text-sm text-gray-400 dark:border-dark-700/60 dark:text-dark-500">
                -
              </td>
            </template>
          </template>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelIcon from '@/components/common/ModelIcon.vue'
import PlazaPriceComparisonCell from './PlazaPriceComparison.vue'
import { getTokenComparisons, getUnitComparisons } from './plazaPriceComparison'
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
  /** 选中的分组；为空时只展示官方价格。 */
  group: ModelPlazaGroup | null
  caption: string
  highlighted?: string
  /** 选中分组的高峰窗口描述（含倍率与时区），用于分时时段 tooltip 披露。 */
  peakWindow?: string
}>()

const { t } = useI18n()

const PER_MILLION = 1_000_000
const MIN_DECIMALS = 2
const TOKEN_FIELDS = ['input_price', 'output_price', 'cache_read_price', 'cache_write_price'] as const

/** 选中分组时本站价列头的倍率标注。 */
const siteRateNote = computed(() => t('modelPlaza.priceTable.siteRateNote', { rate: groupRate() }))

function rowId(m: PlazaModel): string {
  return `plaza-model-${plazaModelKey(m).replace(/[^a-zA-Z0-9_-]/g, '_')}`
}

/** 本站定价的计费模式；只有官方价时取官方价的模式。 */
function billingMode(m: PlazaModel): BillingMode {
  return (m.pricing?.billing_mode || m.official_pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
}

function officialMode(m: PlazaModel): BillingMode {
  return (m.official_pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
}

/** 本站与官方都没有任何价格。 */
function isUnpriced(m: PlazaModel): boolean {
  return !m.pricing && !m.official_pricing
}

/** token 模式的长上下文阶梯；档位的相对倍率在此解析为绝对单价。 */
function tokenIntervals(m: PlazaModel): UserPricingInterval[] {
  if (billingMode(m) !== BILLING_MODE_TOKEN || !m.pricing) return []
  return [...(m.pricing.intervals ?? [])]
    .sort((a, b) => a.min_tokens - b.min_tokens)
    .map((iv) => resolveIntervalPrices(iv, m.pricing!))
}

function isTiered(m: PlazaModel): boolean {
  return (props.group ? tokenIntervals(m) : officialIntervals(m)).length > 1
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
  if (!props.group) return []
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

/**
 * 展示精度：最多保留 DISPLAY_SIGNIFICANT 位有效数字（整数部分不截断），
 * 避免倍率折算或录入换算产生的长小数（如 3.911111111）把列宽撑开。
 */
const DISPLAY_SIGNIFICANT = 5

function roundForDisplay(v: number): number {
  if (!Number.isFinite(v) || v === 0) return v
  const intDigits = Math.floor(Math.log10(Math.abs(v))) + 1
  const precision = Math.min(100, Math.max(DISPLAY_SIGNIFICANT, intDigits + MIN_DECIMALS))
  return Number(v.toPrecision(precision))
}

function money(m: PlazaModel, value: number, scale: number): string {
  const s = formatScaled(roundForDisplay(value * scale), 1, MIN_DECIMALS)
  return isCny(m) ? s.replace(/^\$/, '¥') : s
}

/** 分组生效倍率：用户专属倍率优先。 */
function groupRate(): number {
  if (!props.group) return 1
  return props.group.user_rate_multiplier ?? props.group.rate_multiplier
}

function independentRate(m: PlazaModel): number | null {
  if (billingMode(m) === BILLING_MODE_IMAGE && props.group?.image_rate_independent) return props.group.image_rate_multiplier
  if (billingMode(m) === BILLING_MODE_VIDEO && props.group?.video_rate_independent) return props.group.video_rate_multiplier
  return null
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

/** 官方价按 token 四列展示：本站与官方都是 token 计费且有官方价。 */
function officialTokenRow(m: PlazaModel): boolean {
  return billingMode(m) === BILLING_MODE_TOKEN && officialMode(m) === BILLING_MODE_TOKEN && officialPrices(m) != null
}

function official(m: PlazaModel, value: number | null | undefined): string {
  return value == null ? '-' : money(m, value, PER_MILLION)
}

/** 官方长上下文阶梯；国产模型的渠道价即官方价，阶梯与本站价一致。按张/按秒的官方档位不算阶梯。 */
function officialIntervals(m: PlazaModel): UserPricingInterval[] {
  if (isCny(m)) return tokenIntervals(m)
  if (officialMode(m) !== BILLING_MODE_TOKEN) return []
  return [...(m.official_pricing?.intervals ?? [])].sort((a, b) => a.min_tokens - b.min_tokens)
}

function official1hPrice(m: PlazaModel): number | null {
  const p = isCny(m) ? m.pricing : m.official_pricing
  const v = p?.cache_write_1h_price
  return v != null && v !== p?.cache_write_price ? v : null
}

function unitSuffix(mode: BillingMode): string {
  if (mode === BILLING_MODE_IMAGE) return t('modelPlaza.table.perUnitImage')
  if (mode === BILLING_MODE_VIDEO) return t('modelPlaza.table.perUnitSecond')
  return t('modelPlaza.table.perUnitRequest')
}

function unitBadge(mode: BillingMode): string {
  if (mode === BILLING_MODE_IMAGE) return t('modelPlaza.table.perImage')
  if (mode === BILLING_MODE_VIDEO) return t('modelPlaza.table.perSecond')
  return t('modelPlaza.table.perRequest')
}

/** 按次/按张/按秒价：有档位（尺寸、分辨率）时逐档列出，否则取默认单价。 */
function unitPrices(
  p: { per_request_price?: number | null; intervals?: UserPricingInterval[] } | null | undefined
): Array<{ label: string; price: number }> {
  const tiers = (p?.intervals ?? [])
    .filter((iv) => iv.per_request_price != null)
    .map((iv) => ({ label: iv.tier_label ?? '', price: iv.per_request_price as number }))
  if (tiers.length) return tiers
  const flat = p?.per_request_price
  return flat == null ? [] : [{ label: '', price: flat }]
}

/** 官方的按张/按秒单价；token 计费的官方价走四列展示。 */
function officialUnitPrices(m: PlazaModel): Array<{ label: string; price: number }> {
  return officialMode(m) === BILLING_MODE_TOKEN ? [] : unitPrices(m.official_pricing)
}

/** 分时时段只影响本站价，全部分组时不展示。 */
function timePeriods(m: PlazaModel): PlazaTimePricingPeriod[] {
  return props.group ? m.time_pricing?.periods ?? [] : []
}

/** “00:30–08:30”；整分钟的 HH:mm:ss 省略秒。 */
function formatTimeWindow(p: PlazaTimePricingPeriod): string {
  const clock = (v: string) => v.replace(/^(\d{2}:\d{2}):00$/, '$1')
  return `${clock(p.start_time)}–${clock(p.end_time)}`
}

/** 分时时段说明：时区、仅工作日，以及与高峰时段叠加的披露。 */
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
  /* 固定列需要与卡片同色的不透明底，行悬停/高亮色叠加在它上面 */
  --pz-surface: theme('colors.white');
  --pz-row-hover: theme('colors.gray.50 / 70%');
  --pz-row-highlight: theme('colors.primary.50 / 70%');
  --pz-sticky-edge: theme('colors.gray.100');
}

/* 不能写成 :global(.dark) .plaza-vendor-price-table：Vue 的 scoped 编译会把整条选择器
   换成 .dark，变量落到 <html> 上，又被上面直接写在表格上的浅色值盖掉 */
.dark .plaza-vendor-price-table {
  --pz-title: theme('colors.primary.300');
  --pz-bg: theme('colors.primary.500 / 8%');
  --pz-bg-hover: theme('colors.primary.500 / 14%');
  /* 卡片是半透明 dark-800 叠在 dark-950 页面上 */
  --pz-surface: color-mix(in srgb, theme('colors.dark.800') 50%, theme('colors.dark.950'));
  --pz-row-hover: theme('colors.dark.800 / 50%');
  --pz-row-highlight: theme('colors.primary.500 / 10%');
  --pz-sticky-edge: theme('colors.dark.700 / 60%');
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

/* 窄屏横向滚动时模型名一列固定在左侧；边线用阴影，折叠边框不会跟随 sticky 单元格移动 */
.pz-sticky {
  position: sticky;
  left: 0;
  z-index: 1;
  background-color: var(--pz-surface);
  box-shadow: inset -1px 0 0 var(--pz-sticky-edge);
}

.pz-sticky-head {
  background-color: var(--pz-head-surface, var(--pz-surface));
}

tbody tr:hover > .pz-sticky {
  background-image: linear-gradient(var(--pz-row-hover), var(--pz-row-hover));
}

tbody tr.pz-row-highlight > .pz-sticky {
  background-image: linear-gradient(var(--pz-row-highlight), var(--pz-row-highlight));
}

.pz-title {
  color: var(--pz-title);
  border-color: color-mix(in srgb, var(--pz-title) 30%, transparent);
}

.pz-unit {
  color: color-mix(in srgb, var(--pz-title) 62%, transparent);
}

.pz-rate-badge {
  color: var(--pz-title);
  background: var(--public-accent-soft, var(--pz-bg));
}

.pz-comparison-heading { background: var(--pz-surface); }
</style>
