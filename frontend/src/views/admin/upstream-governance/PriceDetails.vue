<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RemotePrice } from '@/api/admin/upstream-governance'
import { providerLabel } from './providers'

// open starts the price list expanded (the pricing page lists one group at a time).
const props = defineProps<{ prices?: RemotePrice[]; open?: boolean }>()
const { t } = useI18n()
const query = ref('')
const showRaw = ref(false)

type Kind = 'token' | 'request' | 'ratio' | 'other'
type ColumnKey = 'input' | 'output' | 'cacheRead' | 'cacheWrite' | 'cacheWrite1h' | 'perRequest' | 'modelRatio' | 'completionRatio' | 'groupRatio' | 'cacheRatio'
type Cells = Partial<Record<ColumnKey, string>>
interface Row {
  key: string
  kind: Kind
  model: string
  platform: string
  tags: string[]
  cells: Cells
  tiers: { label: string; cells: Cells }[]
  unitLabel: string
  details?: Record<string, unknown>
}
interface Column { key: ColumnKey; label: string; test?: string }

// Units whose amounts are USD per token; they are shown per million tokens.
const tokenUnits = new Set(['usd_per_token', 'sub2api_complex_pricing'])
const searchThreshold = 8
const numberFormat = new Intl.NumberFormat('en-US', { maximumSignificantDigits: 6 })

function num(source: Record<string, unknown> | undefined, key: string): number | null {
  const value = source?.[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}
function amount(value: number | null | undefined): string | undefined {
  if (value == null || !Number.isFinite(value)) return undefined
  return numberFormat.format(Number(value.toPrecision(12)))
}
const perMillion = (value: number | null) => amount(value == null ? null : value * 1_000_000)
const display = (value: string | undefined) => value ?? '—'

function tokenCells(source: Record<string, unknown> | undefined, input: number | null, output: number | null, perRequest: number | null): Cells {
  // Tier rows may carry a multiplier of the base price instead of a price.
  const priced = (price: string, multiplier: string) => {
    const value = num(source, price)
    if (value != null) return perMillion(value)
    const ratio = amount(num(source, multiplier))
    return ratio === undefined ? undefined : `×${ratio}`
  }
  return {
    input: input != null ? perMillion(input) : priced('input_price', 'input_multiplier'),
    output: output != null ? perMillion(output) : priced('output_price', 'output_multiplier'),
    cacheRead: priced('cache_read_price', 'cache_read_multiplier'),
    cacheWrite: priced('cache_write_price', 'cache_write_multiplier'),
    cacheWrite1h: perMillion(num(source, 'cache_write_1h_price')),
    perRequest: amount(perRequest ?? num(source, 'per_request_price')),
  }
}
function tokenCount(value: number | null): string {
  if (value == null) return ''
  if (value >= 1_000_000 && value % 100_000 === 0) return `${value / 1_000_000}M`
  if (value >= 1_000 && value % 100 === 0) return `${value / 1_000}K`
  return String(value)
}
function tierLabel(tier: Record<string, unknown>): string {
  if (typeof tier.tier_label === 'string' && tier.tier_label.trim()) return tier.tier_label.trim()
  // A tier starting at 0 tokens has no lower bound worth showing.
  const min = tokenCount(num(tier, 'min_tokens') || null), max = tokenCount(num(tier, 'max_tokens'))
  if (min && max) return `${min}–${max}`
  return max ? `≤${max}` : min ? `>${min}` : '—'
}
const isRecord = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const present = (value: unknown) => Array.isArray(value) ? value.length > 0 : isRecord(value) ? Object.keys(value).length > 0 : value != null && value !== false && value !== ''

function tags(details: Record<string, unknown> | undefined, tierCount: number): string[] {
  if (!details) return []
  const out: string[] = []
  if (tierCount) out.push(t('governance.priceTable.tiered', { count: tierCount }))
  if (details.long_context_pricing_enabled === true) out.push(t('governance.priceTable.longContext'))
  if (present(details.time_pricing)) out.push(t('governance.priceTable.timePricing'))
  if (details.image_rate_independent === true) out.push(t('governance.priceTable.imageIndependent'))
  if ((num(details, 'image_input_price') ?? 0) > 0 || (num(details, 'image_output_price') ?? 0) > 0) out.push(t('governance.priceTable.imagePrice'))
  if (present(details.reasoning_effort_multipliers)) out.push(t('governance.priceTable.reasoningEffort'))
  if (present(details.billing_plugin_variants)) out.push(t('governance.priceTable.plugins'))
  if (typeof details.channel === 'string' && details.channel.trim()) out.push(t('governance.priceTable.channel', { name: details.channel.trim() }))
  return out
}

function toRow(price: RemotePrice, index: number): Row {
  const details = isRecord(price.details) ? price.details : undefined
  const platform = price.platform && price.platform !== 'unknown' ? providerLabel(price.platform) : ''
  const base = { key: `${index}:${price.model}`, model: price.model, platform, details, tiers: [], unitLabel: '' }
  if (tokenUnits.has(price.unit)) {
    const intervals = Array.isArray(details?.intervals) ? details.intervals.filter(isRecord) : []
    return {
      ...base,
      kind: 'token',
      tags: tags(details, intervals.length),
      cells: intervals.length ? {} : tokenCells(details, price.input, price.output, price.per_request),
      tiers: intervals.map(tier => ({ label: tierLabel(tier), cells: tokenCells(tier, null, null, null) })),
    }
  }
  if (price.unit === 'usd_per_request') {
    return { ...base, kind: 'request', tags: tags(details, 0), cells: { perRequest: amount(price.per_request ?? num(details, 'per_request_price') ?? num(details, 'model_price')) } }
  }
  if (price.unit === 'newapi_ratio') {
    return {
      ...base,
      kind: 'ratio',
      tags: tags(details, 0),
      cells: {
        modelRatio: amount(price.input ?? num(details, 'model_ratio')),
        completionRatio: amount(price.output ?? num(details, 'completion_ratio')),
        groupRatio: amount(num(details, 'pricing_group_ratio')),
        cacheRatio: amount(num(details, 'cache_ratio')),
      },
    }
  }
  return {
    ...base,
    kind: 'other',
    tags: tags(details, 0),
    cells: {},
    unitLabel: price.unit === 'newapi_unknown' ? t('governance.priceTable.complexPricing') : price.unit || t('governance.unknown'),
  }
}

const rows = computed(() => (props.prices || []).map(toRow))
const visibleRows = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return needle ? rows.value.filter(row => row.model.toLowerCase().includes(needle)) : rows.value
})
// A tag or platform every row of a section shares is shown once in the
// section header, so rows stay on one line.
function withSharedTraits<T extends { rows: Row[] }>(section: T) {
  const list = section.rows
  const sharedTags = list.length > 1 ? list[0]!.tags.filter(tag => list.every(row => row.tags.includes(tag))) : []
  const sharedPlatform = list.length > 1 && list.every(row => row.platform === list[0]!.platform) ? list[0]!.platform : ''
  return {
    ...section,
    sharedTags,
    sharedPlatform,
    rows: list.map(row => ({ ...row, tags: row.tags.filter(tag => !sharedTags.includes(tag)), platform: sharedPlatform ? '' : row.platform })),
  }
}
const sections = computed(() => {
  const of = (kind: Kind) => visibleRows.value.filter(row => row.kind === kind)
  const token = of('token')
  const anyCell = (key: ColumnKey) => token.some(row => row.cells[key] || row.tiers.some(tier => tier.cells[key]))
  const tokenColumns: Column[] = [
    { key: 'input', label: t('governance.input'), test: 'price-input' },
    { key: 'output', label: t('governance.output'), test: 'price-output' },
    { key: 'cacheRead', label: t('governance.priceTable.cacheRead') },
    { key: 'cacheWrite', label: t('governance.priceTable.cacheWrite') },
  ]
  if (anyCell('cacheWrite1h')) tokenColumns.push({ key: 'cacheWrite1h', label: t('governance.priceTable.cacheWrite1h') })
  if (anyCell('perRequest')) tokenColumns.push({ key: 'perRequest', label: t('governance.request') })
  return [
    { kind: 'token', title: t('governance.usdPerMillionTokens'), columns: tokenColumns, rows: token },
    { kind: 'request', title: t('governance.usdPerRequest'), columns: [{ key: 'perRequest', label: t('governance.priceTable.price') }] as Column[], rows: of('request') },
    {
      kind: 'ratio',
      title: t('governance.newapiRatio'),
      columns: [
        { key: 'modelRatio', label: t('governance.priceTable.modelRatio') },
        { key: 'completionRatio', label: t('governance.priceTable.completionRatio') },
        { key: 'groupRatio', label: t('governance.priceTable.groupRatio') },
        { key: 'cacheRatio', label: t('governance.priceTable.cacheRatio') },
      ] as Column[],
      rows: of('ratio'),
    },
    { kind: 'other', title: t('governance.priceTable.otherPricing'), columns: [] as Column[], rows: of('other') },
  ].filter(section => section.rows.length).map(withSharedTraits)
})
</script>
<template>
  <details class="text-xs" :open="open">
    <summary class="cursor-pointer text-primary-700 dark:text-primary-300">
      {{ t('governance.prices') }}
      <span class="text-gray-400">{{ prices?.length || '—' }}</span>
    </summary>
    <p v-if="!prices?.length" class="mt-2 text-gray-500">
      {{ t('governance.unknown') }}
    </p>
    <div v-else class="mt-2 space-y-3">
      <p class="text-gray-500">{{ t('governance.priceMultiplierHint') }}</p>
      <div class="flex flex-wrap items-center gap-3">
        <input
          v-if="prices.length > searchThreshold"
          v-model="query"
          data-test="price-search"
          class="input min-w-0 flex-1 basis-40 py-1.5 text-xs"
          :placeholder="t('governance.priceTable.search')"
          :aria-label="t('governance.priceTable.search')"
        />
        <label class="governance-checkbox-label flex items-center gap-2 text-gray-500">
          <input v-model="showRaw" data-test="price-show-raw" type="checkbox" class="governance-checkbox" />
          {{ t('governance.priceTable.showRaw') }}
        </label>
      </div>
      <section v-for="section in sections" :key="section.kind" :data-test="'price-section-' + section.kind" class="min-w-0">
        <div class="mb-1 flex flex-wrap items-center gap-x-2 gap-y-1">
          <h5 class="font-medium text-gray-600 dark:text-gray-300">{{ section.title }}<span v-if="section.sharedPlatform" class="font-normal text-gray-400"> · {{ section.sharedPlatform }}</span></h5>
          <span v-if="section.sharedTags.length" data-test="price-shared-tags" class="flex flex-wrap items-center gap-1 text-gray-400">
            {{ t('governance.priceTable.appliesToAll') }}
            <span v-for="tag in section.sharedTags" :key="tag" class="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ tag }}</span>
          </span>
        </div>
        <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
          <table class="w-full text-left tabular-nums">
            <thead class="bg-gray-50 text-gray-500 dark:bg-dark-900">
              <tr>
                <th class="px-2 py-1.5 font-medium">{{ t('governance.priceTable.model') }}</th>
                <th v-for="column in section.columns" :key="column.key" class="whitespace-nowrap px-2 py-1.5 text-right font-medium">{{ column.label }}</th>
                <th v-if="section.kind === 'other'" class="px-2 py-1.5 font-medium">{{ t('governance.priceTable.note') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <template v-for="row in section.rows" :key="row.key">
                <tr data-test="price-row">
                  <td class="min-w-40 px-2 py-1.5 align-top">
                    <span class="break-all font-medium text-gray-800 dark:text-gray-100">{{ row.model }}</span>
                    <span v-if="row.platform" class="ml-1.5 text-gray-400">{{ row.platform }}</span>
                    <span v-if="row.tags.length" class="mt-1 flex flex-wrap gap-1">
                      <span v-for="tag in row.tags" :key="tag" data-test="price-tag" class="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ tag }}</span>
                    </span>
                  </td>
                  <td v-for="column in section.columns" :key="column.key" :data-test="column.test" class="whitespace-nowrap px-2 py-1.5 text-right align-top">
                    <template v-if="!row.tiers.length">{{ display(row.cells[column.key]) }}</template>
                  </td>
                  <td v-if="section.kind === 'other'" class="px-2 py-1.5 align-top text-gray-500">{{ row.unitLabel }} · {{ t('governance.priceTable.seeRaw') }}</td>
                </tr>
                <tr v-for="(tier, tierIndex) in row.tiers" :key="row.key + ':' + tierIndex" data-test="price-tier">
                  <td class="py-1 pl-5 pr-2 text-gray-500">{{ tier.label }}</td>
                  <td v-for="column in section.columns" :key="column.key" class="whitespace-nowrap px-2 py-1 text-right">{{ display(tier.cells[column.key]) }}</td>
                </tr>
                <tr v-if="showRaw && row.details">
                  <td :colspan="section.columns.length + (section.kind === 'other' ? 2 : 1)" class="px-2 pb-2">
                    <pre data-test="price-raw" class="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 text-[11px] text-gray-500 dark:bg-dark-900">{{ JSON.stringify(row.details, null, 2) }}</pre>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
      </section>
      <p v-if="!sections.length" class="text-gray-500">{{ t('governance.priceTable.noMatch') }}</p>
    </div>
  </details>
</template>
