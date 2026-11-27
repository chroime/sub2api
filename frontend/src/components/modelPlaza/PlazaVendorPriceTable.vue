<template>
  <div
    class="plaza-vendor-price-table max-w-full overflow-x-auto rounded-2xl border bg-white shadow-card focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:bg-dark-800/50"
    role="region" tabindex="0" :aria-label="caption"
  >
    <table class="w-full border-separate border-spacing-0 text-sm tabular-nums" :class="showTiers ? 'min-w-[680px]' : 'min-w-[600px]'">
      <caption class="sr-only">{{ caption }}</caption>
      <thead>
        <tr>
          <th scope="col" rowspan="2" class="pz-sticky pz-sticky-head w-[29%] min-w-[250px] px-4 py-3 text-left font-medium">
            {{ t('modelPlaza.table.model') }}
          </th>
          <th v-if="showTiers" scope="col" rowspan="2" class="w-20 min-w-[80px] px-3 py-3 text-left font-medium">{{ t('modelPlaza.priceTable.tier') }}</th>
          <th scope="colgroup" colspan="4" class="px-3 pt-3 text-center font-semibold" :class="{ 'pz-bg': group }">
            {{ t(group ? 'modelPlaza.priceTable.sitePrice' : 'modelPlaza.priceTable.official') }}
            <span class="ml-2 text-xs font-normal text-gray-500 dark:text-dark-300">
              {{ t(unitOnly
                ? group ? 'modelPlaza.priceTable.siteMediaUnitNote' : 'modelPlaza.priceTable.mediaUnitNote'
                : group ? 'modelPlaza.priceTable.siteUnitNote' : 'modelPlaza.priceTable.unitNote') }}
            </span>
          </th>
        </tr>
        <tr class="text-xs text-gray-500 dark:text-dark-300">
          <th v-if="unitOnly" scope="col" colspan="4" class="px-4 py-2 text-right font-medium" :class="{ 'pz-bg': group }">{{ t('modelPlaza.priceTable.unitPrice') }}</th>
          <template v-else>
            <th v-for="field in TOKEN_FIELDS" :key="field" scope="col" class="px-4 py-2 text-right font-medium" :class="{ 'pz-bg': group }">{{ fieldLabel(field) }}</th>
          </template>
        </tr>
      </thead>
      <tbody v-for="{ model, prices } in rows" :key="plazaModelKey(model)" :class="{ 'pz-row-highlight': highlighted === plazaModelKey(model) }">
        <tr v-for="(row, index) in prices" :id="index === 0 ? rowId(model) : undefined" :key="row.key" :data-price-row="group ? 'site' : 'official'">
          <th v-if="index === 0" scope="rowgroup" :rowspan="prices.length" class="pz-sticky px-4 py-3 text-left align-top font-normal">
            <div class="flex items-start gap-2">
              <ModelIcon :model="model.name" size="16px" class="mt-0.5 shrink-0" />
              <div class="min-w-0 flex-1">
                <span class="block break-words font-mono text-sm font-semibold text-gray-900 dark:text-white [overflow-wrap:anywhere]">{{ model.name }}</span>
                <PlazaPricingBadges :model="model" :group="group" />
              </div>
              <button
                type="button"
                class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :title="t('modelPlaza.table.copyModelId', { model: model.name })"
                :aria-label="t('modelPlaza.table.copyModelId', { model: model.name })"
                data-copy-model-id
                @click="copyToClipboard(model.name)"
              >
                <Icon name="copy" size="sm" aria-hidden="true" />
              </button>
            </div>
            <div v-if="group && hasPricingRules(model)" class="mt-2 max-w-[11rem] space-y-1.5 break-words whitespace-normal text-xs leading-5" data-pricing-rules>
              <p v-if="model.long_context_basis === 'marginal'" class="rounded-md bg-violet-50 px-2 py-1 text-violet-700 dark:bg-violet-500/10 dark:text-violet-300" data-marginal-pricing>
                {{ t('modelPlaza.table.tierHintMarginal') }}
              </p>
              <div v-if="reasoningMultipliers(model).length" class="rounded-md bg-blue-50 px-2 py-1 text-blue-700 dark:bg-blue-500/10 dark:text-blue-300" data-reasoning-pricing>
                <span class="font-medium">{{ t('modelPlaza.priceTable.reasoningRates') }}</span>
                <div class="flex flex-wrap gap-x-2">
                  <span v-for="[effort, multiplier] in reasoningMultipliers(model)" :key="effort" :data-reasoning-effort="effort">{{ t('modelPlaza.table.reasoningMultiplierBadge', { effort, multiplier }) }}</span>
                </div>
              </div>

            </div>
          </th>
          <th v-if="showTiers" scope="row" class="px-3 py-3 text-left align-top text-xs font-normal text-gray-500 dark:text-dark-300" data-price-tier>
            {{ row.label || '—' }}
          </th>
          <td v-if="row.unpriced" colspan="4" class="px-4 py-3 text-center text-xs text-gray-500 dark:text-dark-300" data-unpriced>{{ t('modelPlaza.priceTable.unpriced') }}</td>
          <td v-else-if="row.unit" colspan="4" class="px-3 py-3 text-right align-top" :class="{ 'pz-cell': group }">
            <span v-if="!showTiers && row.label" class="mr-2 text-xs text-gray-500 dark:text-dark-300">{{ row.label }}</span>
            <div class="pz-price-stack ml-auto w-fit text-right">
              <div v-if="group" class="flex items-baseline justify-between gap-2 font-mono text-xs font-normal leading-5 text-gray-500 dark:text-dark-300" :title="t('modelPlaza.priceTable.official')" data-official-reference>
                <span class="font-sans">{{ t('modelPlaza.priceTable.officialLabel') }}</span><span>{{ displayOfficialPrice(row.columns[0][0]) }}<span class="ml-1 font-sans">{{ row.unit }}</span></span>
              </div>
              <div v-if="group" class="flex items-baseline justify-between gap-2 font-mono text-xs font-normal leading-5 text-gray-500 dark:text-dark-300" :title="referenceTitle(model, row.columns[0][0])" data-reference-price :data-reference-source="referenceSource(model, row.columns[0][0])">
                <span class="font-sans">{{ t('modelPlaza.priceTable.referenceLabel') }}</span><span>{{ displayReferencePrice(model, row.columns[0][0]) }}<span class="ml-1 font-sans">{{ row.unit }}</span></span>
              </div>
              <div class="font-mono font-semibold" :class="group ? 'pz-payable mt-1 flex items-baseline justify-between gap-2 text-2xl leading-9' : 'text-base'">
                <span v-if="group" class="font-sans text-xs font-normal text-gray-500 dark:text-dark-300">{{ t('modelPlaza.priceTable.paidLabel') }}</span><span :data-site-price="group ? '' : undefined">{{ displayPrice(row.columns[0][0]) }}<span class="ml-1 font-sans text-xs font-normal">{{ row.unit }}</span></span>
              </div>
              <div v-if="group" class="mt-2 flex justify-end">
                <div v-if="savings(row.columns[0][0])" class="pz-savings-highlight" :class="{ 'pz-savings-higher': savings(row.columns[0][0])!.status === 'higher' }" :title="savingsBasis" data-savings data-savings-reference="official">
                  <template v-if="savings(row.columns[0][0])!.status !== 'equal'">
                    <span><span class="mr-1 font-sans text-[11px]">{{ savingsLabel(savings(row.columns[0][0])!.status) }}</span><span class="font-mono text-lg font-bold tabular-nums">{{ savings(row.columns[0][0])!.percent }}</span></span>
                    <span class="font-mono text-sm font-semibold tabular-nums">{{ savings(row.columns[0][0])!.amount }}</span>
                  </template>
                  <span v-else class="text-xs">{{ t('modelPlaza.comparison.sameOfficialPrice') }}</span>
                </div>
                <span v-else class="pz-savings-unavailable">{{ unavailableSavingsLabel(row.columns[0][0]) }}</span>
              </div>
            </div>
          </td>
          <td v-for="(column, columnIndex) in row.unit || row.unpriced ? [] : row.columns" :key="columnIndex" class="px-3 py-3 text-right align-top" :class="{ 'pz-cell': group }" :data-price-field="TOKEN_FIELDS[columnIndex]">
            <div v-for="price in column" :key="price.key" class="flex justify-end whitespace-nowrap leading-6">
              <div class="pz-price-stack w-full text-right">
                <span v-if="cacheLifetime(price)" class="mb-1 block text-left text-[11px] leading-4 text-gray-500 dark:text-dark-300" data-cache-lifetime>{{ cacheLifetime(price) }}</span>
                <div v-if="group" class="flex items-baseline justify-between gap-2 font-mono text-xs font-normal leading-5 text-gray-500 dark:text-dark-300" :title="t('modelPlaza.priceTable.official')" data-official-reference>
                  <span class="font-sans">{{ t('modelPlaza.priceTable.officialLabel') }}</span><span>{{ displayOfficialPrice(price) }}</span>
                </div>
                <div v-if="group && price.referenceLabel" class="text-right text-[10px] leading-4 text-gray-500 dark:text-dark-300" data-reference-tier>{{ t('modelPlaza.priceTable.referenceTier', { tier: price.referenceLabel }) }}</div>
                <div v-if="group" class="flex items-baseline justify-between gap-2 font-mono text-xs font-normal leading-5 text-gray-500 dark:text-dark-300" :title="referenceTitle(model, price)" data-reference-price :data-reference-source="referenceSource(model, price)">
                  <span class="font-sans">{{ t('modelPlaza.priceTable.referenceLabel') }}</span><span>{{ displayReferencePrice(model, price) }}</span>
                </div>
                <div class="font-mono font-semibold" :class="group ? 'pz-payable mt-1 flex items-baseline justify-between gap-2 text-2xl leading-9' : 'text-base'"><span v-if="group" class="font-sans text-xs font-normal text-gray-500 dark:text-dark-300">{{ t('modelPlaza.priceTable.paidLabel') }}</span><span :data-site-price="group ? '' : undefined">{{ displayPrice(price) }}</span></div>
                <div v-if="group" class="mt-2 flex justify-end">
                  <div v-if="savings(price)" class="pz-savings-highlight" :class="{ 'pz-savings-higher': savings(price)!.status === 'higher' }" :title="savingsTitle(price)" data-savings data-savings-reference="official">
                    <template v-if="savings(price)!.status !== 'equal'">
                      <span><span class="mr-1 font-sans text-[11px]">{{ savingsLabel(savings(price)!.status) }}</span><span class="font-mono text-lg font-bold tabular-nums">{{ savings(price)!.percent }}</span></span>
                      <span class="font-mono text-sm font-semibold tabular-nums">{{ savings(price)!.amount }}</span>
                    </template>
                    <span v-else class="text-xs">{{ t('modelPlaza.comparison.sameOfficialPrice') }}</span>
                  </div>
                  <span v-else class="pz-savings-unavailable">{{ unavailableSavingsLabel(price) }}</span>
                </div>
              </div>
            </div>
            <span v-if="!column.length" class="text-gray-400 dark:text-dark-400">—</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import PlazaPricingBadges from './PlazaPricingBadges.vue'
import { useI18n } from 'vue-i18n'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import {
  getTokenComparisons, getUnitComparisons, getPlazaPayableComparison, comparePriceAmounts,
  PLAZA_REFERENCE_USD_TO_CNY, PLAZA_CNY_PER_USD_CREDIT, type PlazaPriceComparison, type TokenPriceField
} from './plazaPriceComparison'
import { formatScaled } from '@/utils/pricing'
import { BILLING_MODE_TOKEN, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO, REASONING_EFFORT_LEVELS, type BillingMode } from '@/constants/channel'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import { plazaModelKey } from './plazaVendorTree'

const props = defineProps<{
  models: PlazaModel[]
  group: ModelPlazaGroup | null
  caption: string
  highlighted?: string
  peakWindow?: string
}>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const TOKEN_FIELDS = ['input_price', 'output_price', 'cache_read_price', 'cache_write_price'] as const
const savingsBasis = computed(() => t('modelPlaza.comparison.officialBasis', {
  rate: PLAZA_REFERENCE_USD_TO_CNY, creditRate: PLAZA_CNY_PER_USD_CREDIT
}))

interface PriceRow {
  key: string
  label: string
  columns: PlazaPriceComparison[][]
  unit?: string
  unpriced?: boolean
}

function billingMode(model: PlazaModel): BillingMode {
  const pricing = props.group ? model.pricing : model.official_pricing
  return pricing?.billing_mode || BILLING_MODE_TOKEN
}

function priceRows(model: PlazaModel): PriceRow[] {
  const referenceSource = props.group ? 'official-matched' : 'official'
  if (billingMode(model) !== BILLING_MODE_TOKEN) {
    return getUnitComparisons(model, props.group, referenceSource)
      .filter(price => (props.group ? price.site : price.reference) != null)
      .map(price => ({ key: price.key, label: price.label, columns: [[price]], unit: unitSuffix(price.billingMode) }))
  }
  const result = new Map<string, PriceRow>()
  TOKEN_FIELDS.forEach((field, column) => {
    for (const price of getTokenComparisons(model, props.group, field, referenceSource)) {
      if ((props.group ? price.site : price.reference) == null) continue
      const key = price.key.slice(field.length + 1).replace(/:(?:1h|12h)$/, '')
      let row = result.get(key)
      if (!row) {
        const label = cacheLifetime(price) ? price.label.replace(/(?: · )?(?:1h|12h)$/, '') : price.label
        row = { key, label, columns: TOKEN_FIELDS.map(() => []) }
        result.set(key, row)
      }
      row.columns[column].push(price)
    }
  })
  return [...result.values()]
}

const rows = computed(() => props.models.map(model => {
  const prices = priceRows(model)
  return { model, prices: prices.length ? prices : [{ key: 'unpriced', label: '', columns: [], unpriced: true } as PriceRow] }
}))
const showTiers = computed(() => rows.value.some(row => row.prices.length > 1 || row.prices.some(price => price.label)))
const unitOnly = computed(() => props.models.length > 0 && props.models.every(model => billingMode(model) !== BILLING_MODE_TOKEN))

function rowId(model: PlazaModel): string {
  return `plaza-model-${plazaModelKey(model).replace(/[^a-zA-Z0-9_-]/g, '_')}`
}

function cacheLifetime(price: PlazaPriceComparison): string {
  return price.key.match(/:(1h|12h)$/)?.[1] || ''
}

function displayPrice(price: PlazaPriceComparison): string {
  const currency = props.group ? (price.siteCurrency ?? price.currency) : (price.referenceCurrency ?? price.currency)
  let amount = props.group ? price.site : price.reference
  if (amount == null || !Number.isFinite(amount)) return '—'
  const digits = amount === 0 ? 1 : Math.floor(Math.log10(Math.abs(amount))) + 1
  const rounded = Number(amount.toPrecision(Math.min(100, Math.max(5, digits + 2))))
  const formatted = formatScaled(rounded, 1, 2)
  return formatted.replace(/^\$/, currency === 'CNY' ? '¥' : currency === 'USD' ? '$' : '')
}


function fieldLabel(field: TokenPriceField): string {
  const keys = { input_price: 'table.input', output_price: 'table.output', cache_read_price: 'priceTable.cacheRead', cache_write_price: 'priceTable.cacheWrite' }
  return t(`modelPlaza.${keys[field]}`)
}

function displayReferencePrice(model: PlazaModel, price: PlazaPriceComparison): string {
  const currency = referenceSource(model, price) !== 'official'
    ? (price.siteCurrency ?? price.currency) : (price.referenceCurrency ?? price.currency)
  const amount = referenceSource(model, price) !== 'official' ? price.configured : price.reference
  if (amount == null || !Number.isFinite(amount)) return '—'
  const digits = amount === 0 ? 1 : Math.floor(Math.log10(Math.abs(amount))) + 1
  const rounded = Number(amount.toPrecision(Math.min(100, Math.max(5, digits + 2))))
  const formatted = formatScaled(rounded, 1, 2)
  return formatted.replace(/^\$/, currency === 'CNY' ? '¥' : currency === 'USD' ? '$' : '')
}

function displayOfficialPrice(price: PlazaPriceComparison): string {
  const currency = price.referenceCurrency ?? price.currency
  const amount = price.reference
  if (amount == null || !Number.isFinite(amount)) return '—'
  const digits = amount === 0 ? 1 : Math.floor(Math.log10(Math.abs(amount))) + 1
  const rounded = Number(amount.toPrecision(Math.min(100, Math.max(5, digits + 2))))
  const formatted = formatScaled(rounded, 1, 2)
  return formatted.replace(/^\$/, currency === 'CNY' ? '¥' : currency === 'USD' ? '$' : '')
}

function referenceSource(model: PlazaModel, price: PlazaPriceComparison): 'group' | 'channel' | 'official' {
  const source = model.pricing_source
  return (source === 'group' || source === 'channel') && price.configured != null && Number.isFinite(price.configured)
    ? source : 'official'
}

function referenceTitle(model: PlazaModel, price: PlazaPriceComparison): string {
  const source = referenceSource(model, price)
  return t(source === 'official' ? 'modelPlaza.priceTable.official' : `modelPlaza.priceTable.source_${source}`)
}

type SavingsStatus = 'saving' | 'higher' | 'equal'
interface SavingsResult { status: SavingsStatus; amount: string; percent: string }

function savings(price: PlazaPriceComparison): SavingsResult | null {
  if (!props.group || !price.comparable) return null
  const comparison = getPlazaPayableComparison(price)
  if (!comparison.comparable || comparison.reference == null || comparison.site == null) return null
  const result = comparePriceAmounts(comparison.reference, comparison.site)
  if (result.difference == null || result.status === 'unavailable') return null
  const status = result.status
  const amount = formatCny(Math.abs(result.difference))
  const percentage = result.percent == null ? null : Math.abs(result.percent)
  const percent = percentage == null ? '—' : (status === 'saving' && comparison.site > 0 && percentage >= 99.995 ? '>99.99%' : `${percentage.toFixed(2)}%`)
  return { status, amount, percent }
}

function formatCny(value: number): string {
  const digits = value === 0 ? 1 : Math.floor(Math.log10(Math.abs(value))) + 1
  const rounded = Number(value.toPrecision(Math.min(100, Math.max(5, digits + 2))))
  return formatScaled(rounded, 1, 2).replace(/^\$/, '¥')
}

function savingsLabel(status: SavingsStatus): string {
  return t(status === 'saving' ? 'modelPlaza.priceTable.savingLabel' : status === 'higher' ? 'modelPlaza.priceTable.surchargeLabel' : 'modelPlaza.priceTable.equalLabel')
}

function unavailableSavingsLabel(price: PlazaPriceComparison): string {
  return t(price.reference != null ? 'modelPlaza.comparison.differentBasis' : 'modelPlaza.comparison.noOfficialReference')
}

function savingsTitle(price: PlazaPriceComparison): string {
  return [price.referenceLabel ? t('modelPlaza.priceTable.referenceTier', { tier: price.referenceLabel }) : '', savingsBasis.value]
    .filter(Boolean).join(' · ')
}

function unitSuffix(mode: BillingMode): string {
  if (mode === BILLING_MODE_IMAGE) return t('modelPlaza.table.perUnitImage')
  if (mode === BILLING_MODE_VIDEO) return t('modelPlaza.table.perUnitSecond')
  return t('modelPlaza.table.perUnitRequest')
}

function hasPricingRules(model: PlazaModel): boolean {
  return model.long_context_basis === 'marginal' || reasoningMultipliers(model).length > 0
}

function reasoningMultipliers(model: PlazaModel): [string, number][] {
  const configured = model.pricing?.reasoning_effort_multipliers
  if (!configured) return []
  return REASONING_EFFORT_LEVELS.flatMap(effort => {
    const multiplier = configured[effort]
    return typeof multiplier === 'number' && Number.isFinite(multiplier) && multiplier > 0 ? [[effort, multiplier] as [string, number]] : []
  })
}


defineExpose({ rowId })
</script>

<style scoped>
.plaza-vendor-price-table {
  --pz-grid: theme('colors.slate.200');
  --pz-divider: theme('colors.slate.300');
  --pz-surface: theme('colors.white');
  --pz-head: theme('colors.slate.50');
  --pz-bg: theme('colors.primary.50 / 40%');
  --pz-accent: theme('colors.primary.700');
  border-color: var(--pz-divider);
}
.dark .plaza-vendor-price-table {
  --pz-grid: theme('colors.slate.700');
  --pz-divider: theme('colors.slate.600');
  --pz-surface: theme('colors.dark.800');
  --pz-head: theme('colors.dark.800');
  --pz-bg: theme('colors.primary.500 / 6%');
  --pz-accent: theme('colors.primary.300');
}
thead th { background: var(--pz-head); border-bottom: 1px solid var(--pz-grid); }
tbody td + td { border-left: 1px solid var(--pz-grid); }
tbody + tbody > tr:first-child > :is(th, td) { border-top: 1px solid var(--pz-divider); }
tbody tr + tr > :is(th, td) { border-top: 1px solid var(--pz-grid); }
.pz-bg, .pz-cell { background: var(--pz-bg); }
.pz-payable { color: var(--pz-accent); }
.pz-sticky { position: sticky; left: 0; z-index: 1; background: var(--pz-surface); box-shadow: inset -1px 0 0 var(--pz-divider); }
.pz-sticky-head { background: var(--pz-head); }
.pz-row-highlight .pz-sticky { background: var(--pz-bg); }
.pz-cell .pz-price-stack { min-width: 8rem; }
.pz-cell > div + div { margin-top: 0.75rem; }
.pz-savings-highlight {
  @apply inline-flex min-h-14 w-full max-w-full flex-col items-end justify-center rounded-lg border border-blue-200 bg-blue-50 px-3 py-1.5 leading-6 text-blue-800 dark:border-blue-800/70 dark:bg-blue-950/50 dark:text-blue-200;
}
.pz-savings-unavailable {
  @apply inline-flex min-h-14 w-full items-center justify-end rounded-lg bg-slate-100/60 px-3 py-1.5 text-xs text-gray-500 dark:bg-slate-700/40 dark:text-dark-300;
}
.pz-savings-higher {
  @apply border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-800/70 dark:bg-rose-950/40 dark:text-rose-200;
}
</style>
