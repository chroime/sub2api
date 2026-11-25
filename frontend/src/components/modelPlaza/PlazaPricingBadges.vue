<template>
  <div class="mt-2 flex flex-wrap gap-1.5" data-pricing-badges>
    <button type="button" class="rule-badge" :class="currency === 'CNY' ? 'rule-cny' : currency === 'USD' ? 'rule-usd' : 'rule-unknown'" data-price-currency :data-currency="currency" :aria-label="t('modelPlaza.pricingRules.details')" @click="open = true">{{ currencyLabel }}</button>
    <button type="button" class="rule-badge" :class="contextStatus === 'tiered' ? 'rule-tiered' : contextStatus === 'unknown' ? 'rule-unknown' : 'rule-neutral'" data-context-rule :data-status="contextStatus" @click="open = true">{{ contextLabel }}</button>
    <button v-if="timeStatus !== 'unknown'" type="button" class="rule-badge" :class="timeStatus === 'peak_offpeak' || timeStatus === 'scheduled' ? 'rule-timed' : 'rule-neutral'" data-time-rule :data-status="timeStatus" @click="open = true">{{ t(`modelPlaza.pricingRules.time_${timeStatus}`) }}</button>
  </div>
  <p v-if="rules?.price_basis && rules.price_basis !== 'standard'" class="mt-1.5 text-xs leading-5 text-gray-500 dark:text-dark-300" data-price-basis>{{ t(`modelPlaza.pricingRules.basis_${rules.price_basis}`) }}</p>
  <p v-if="group && contextStatus === 'flat' && model.official_pricing_rules?.context_status === 'tiered'" class="mt-1.5 text-xs text-gray-500 dark:text-dark-300">{{ t('modelPlaza.pricingRules.officialTiers', { count: model.official_pricing_rules.context_tier_count }) }}</p>
  <BaseDialog :show="open" :title="model.name" width="wide" @close="open = false">
    <div class="space-y-5 text-sm" data-pricing-rule-details>
      <p class="text-gray-500 dark:text-dark-300">{{ group ? t('modelPlaza.priceTable.sitePrice') : t('modelPlaza.priceTable.official') }} · {{ currencyLabel }}</p>
      <section>
        <h4 class="mb-2 font-semibold">{{ contextLabel }}</h4>
        <p class="text-xs leading-6 text-gray-500 dark:text-dark-300">{{ contextStatus === 'unknown' ? t('modelPlaza.pricingRules.unknownHint') : contextStatus === 'not_applicable' ? t('modelPlaza.pricingRules.mediaHint') : t(rules?.context_basis === 'marginal' ? 'modelPlaza.table.tierHintMarginal' : 'modelPlaza.table.tierHint') }}</p>
        <div v-if="contextStatus === 'tiered'" class="mt-3 overflow-x-auto">
          <table class="w-full text-right text-xs tabular-nums"><thead><tr class="text-gray-500"><th class="p-2 text-left">{{ t('modelPlaza.priceTable.tier') }}</th><th v-for="field in fields" :key="field" class="p-2">{{ fieldName(field) }}</th></tr></thead><tbody><tr v-for="(iv, i) in intervals" :key="i" class="border-t border-gray-200 dark:border-dark-700"><td class="p-2 text-left">{{ intervalLabel(iv) }}</td><td v-for="field in fields" :key="field" class="p-2 font-mono">{{ price(intervalPrices(iv)[field]) }}</td></tr></tbody></table>
        </div>
      </section>
      <section>
        <h4 class="mb-2 font-semibold">{{ t(`modelPlaza.pricingRules.time_${timeStatus}`) }}</h4>
        <p v-if="!rules?.time_rules?.length" class="text-xs leading-6 text-gray-500 dark:text-dark-300">{{ t(timeStatus === 'uniform' ? 'modelPlaza.pricingRules.uniformHint' : 'modelPlaza.pricingRules.unknownHint') }}</p>
        <div v-for="(rule, index) in rules?.time_rules" :key="index" class="mb-4 rounded-xl border border-gray-200 p-3 dark:border-dark-600">
          <div class="flex flex-wrap items-center gap-2 font-medium">{{ t(`modelPlaza.pricingRules.source_${rule.source}`) }} <span class="text-xs font-normal text-gray-500">{{ rule.timezone }}<template v-if="rule.weekdays_only"> · {{ t('modelPlaza.table.timePricingWeekdays') }}</template></span></div>
          <p class="mt-2 text-xs leading-6 text-gray-500 dark:text-dark-300">{{ t('modelPlaza.pricingRules.baseMultiplier', { multiplier: rule.base_multiplier }) }}</p>
          <p v-if="rule.current_multiplier != null" class="text-xs font-medium leading-6" :data-current-rule-multiplier="rule.current_multiplier">{{ t('modelPlaza.pricingRules.currentMultiplier', { multiplier: rule.current_multiplier }) }}</p>
          <p v-for="(period, j) in rule.periods" :key="j" class="text-xs leading-6">{{ clock(period.start_time) }}–{{ clock(period.end_time) }} · ×{{ period.multiplier }}</p>
          <div v-if="rule.source === 'official'" class="mt-3 overflow-x-auto">
            <table class="w-full text-right text-xs tabular-nums"><thead><tr class="text-gray-500"><th class="p-2 text-left">{{ t('modelPlaza.pricingRules.period') }}</th><th v-for="field in fields.slice(0,3)" :key="field" class="p-2">{{ fieldName(field) }}</th></tr></thead><tbody><tr v-for="(multiplier, j) in timeMultipliers(rule)" :key="j" class="border-t border-gray-200 dark:border-dark-700"><td class="p-2 text-left">{{ t(j === 0 ? 'modelPlaza.pricingRules.offPeak' : 'modelPlaza.pricingRules.peak') }}</td><td v-for="field in fields.slice(0,3)" :key="field" class="p-2 font-mono">{{ price(timeValue(rule, field, multiplier)) }}</td></tr></tbody></table>
          </div>
        </div>
        <div v-if="rules?.evaluated_at && rules.current_multipliers && rules.time_rules?.length" class="mt-4 rounded-xl border border-gray-200 p-3 dark:border-dark-600" data-current-prices>
          <h4 class="font-semibold">{{ t('modelPlaza.pricingRules.currentPrices') }}</h4>
          <p class="mt-1 text-xs leading-6 text-gray-500 dark:text-dark-300">{{ t('modelPlaza.pricingRules.snapshotHint') }} <time :datetime="rules.evaluated_at">{{ snapshotTime(rules.evaluated_at) }}</time></p>
          <div class="mt-3 overflow-x-auto">
            <table class="w-full text-right text-xs tabular-nums">
              <thead><tr class="text-gray-500"><th class="p-2 text-left">{{ t('modelPlaza.priceTable.tier') }}</th><th v-for="field in fields" :key="field" class="p-2">{{ fieldName(field) }}<span class="block font-normal">×{{ rules.current_multipliers[field] ?? 1 }}</span></th></tr></thead>
              <tbody><tr v-for="(row, i) in currentRows" :key="i" class="border-t border-gray-200 dark:border-dark-700"><td class="p-2 text-left">{{ row.label || '—' }}</td><td v-for="field in fields" :key="field" class="p-2 font-mono">{{ price(currentValue(row.values[field], field)) }}</td></tr></tbody>
            </table>
          </div>
        </div>
        <p v-if="rules?.time_rules?.length" class="text-xs leading-6 text-gray-500 dark:text-dark-300">{{ t('modelPlaza.pricingRules.timeHint') }}</p>
      </section>
      <p class="border-t border-gray-200 pt-3 text-xs text-gray-500 dark:border-dark-700">{{ t('modelPlaza.pricingRules.scopeHint') }}</p>
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { ModelPlazaGroup, PlazaModel, PlazaPricingTimeRule } from '@/api/modelPlaza'
import type { UserPricingInterval } from '@/api/channels'
import { resolveIntervalPrices } from '@/utils/pricing'
import { effectivePlazaRate, type TokenPriceField } from './plazaPriceComparison'
const props = defineProps<{ model: PlazaModel; group: ModelPlazaGroup | null }>()
const { t } = useI18n()
const open = ref(false)
const rules = computed(() => props.group ? props.model.pricing_rules : props.model.official_pricing_rules)
const pricing = computed(() => props.group ? props.model.pricing : props.model.official_pricing)
const currency = computed(() => props.group ? rules.value?.currency || '' : rules.value?.currency || props.model.official_pricing?.currency || '')
function currencyName(value: string) { return t(`modelPlaza.pricingRules.${value === 'CNY' ? 'cny' : value === 'USD' ? 'usd' : 'currencyUnknown'}`) }
const currencyLabel = computed(() => {
  const official = props.model.official_pricing_rules?.currency || props.model.official_pricing?.currency || ''
  return props.group && currency.value && official && currency.value !== official
    ? t('modelPlaza.pricingRules.scopedCurrencies', { site: currencyName(currency.value), official: currencyName(official) })
    : currencyName(currency.value)
})
const contextStatus = computed(() => rules.value?.context_status ?? 'unknown')
const timeStatus = computed(() => rules.value?.time_status ?? 'unknown')
const modeLabels: Record<string,string> = { image: 'perImage', video: 'perSecond', per_request: 'perRequest' }
const contextLabel = computed(() => contextStatus.value === 'not_applicable'
  ? t(`modelPlaza.table.${modeLabels[pricing.value?.billing_mode ?? ''] ?? 'perRequest'}`)
  : t(`modelPlaza.pricingRules.context_${contextStatus.value}`, { count: rules.value?.context_tier_count ?? 0 }))
const intervals = computed(() => pricing.value?.intervals ?? [])
const currentRows = computed<{ label: string; values: Partial<Record<RulePriceField, number | null>> }[]>(() => contextStatus.value === 'tiered'
  ? intervals.value.map(iv => ({ label: intervalLabel(iv), values: intervalPrices(iv) }))
  : [{ label: '', values: pricing.value ?? {} }])
type RulePriceField = TokenPriceField | 'cache_write_1h_price' | 'cache_write_12h_price'
const fields = computed<RulePriceField[]>(() => {
  const result: RulePriceField[] = ['input_price', 'output_price', 'cache_read_price', 'cache_write_price']
  for (const field of ['cache_write_1h_price', 'cache_write_12h_price'] as const) {
    if (pricing.value?.[field] != null || intervals.value.some(iv => iv[field] != null)) result.push(field)
  }
  return result
})
function fieldName(field: RulePriceField) {
  if (field === 'cache_write_1h_price' || field === 'cache_write_12h_price') return t('modelPlaza.priceTable.cacheWrite') + (field === 'cache_write_1h_price' ? ' · 1h' : ' · 12h')
  return t(({input_price:'modelPlaza.table.input',output_price:'modelPlaza.table.output',cache_read_price:'modelPlaza.priceTable.cacheRead',cache_write_price:'modelPlaza.priceTable.cacheWrite'})[field])
}
function price(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  return (currency.value === 'CNY' ? '¥' : currency.value === 'USD' ? '$' : '') + new Intl.NumberFormat('en-US', { minimumFractionDigits:2, maximumFractionDigits:6 }).format(value * 1e6 * (props.group ? effectivePlazaRate(props.model, props.group) : 1))
}
function intervalPrices(iv: UserPricingInterval) {
  if (!props.group || !props.model.pricing) return iv
  return resolveIntervalPrices(iv, props.model.pricing)
}
function intervalLabel(iv: UserPricingInterval) {
  if (iv.tier_label) return iv.tier_label
  const token = (n:number) => n >= 1e6 ? `${n/1e6}M` : n >= 1000 ? `${n/1000}K` : String(n)
  return iv.min_tokens === 0 ? `≤${iv.max_tokens == null ? '∞' : token(iv.max_tokens)}` : iv.max_tokens == null ? `>${token(iv.min_tokens)}` : `>${token(iv.min_tokens)}–${token(iv.max_tokens)}`
}
function clock(value: string) { return value.replace(/:00$/, '').length === 5 ? value.slice(0,5) : value }
function timeMultipliers(rule: PlazaPricingTimeRule) { return [...new Set([rule.base_multiplier, ...rule.periods.map(p => p.multiplier)])] }
function timeValue(rule: PlazaPricingTimeRule, field: RulePriceField, multiplier:number) {
  const value = pricing.value?.[field]
  return value == null ? null : value * (rule.fields.includes(field) ? multiplier : 1)
}
function currentValue(value: number | null | undefined, field: RulePriceField) {
  return value == null ? null : value * (rules.value?.current_multipliers?.[field] ?? 1)
}
function snapshotTime(value: string) {
  return new Intl.DateTimeFormat(undefined, { timeZone: 'Asia/Shanghai', dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value)) + ' (UTC+08:00)'
}
</script>
<style scoped>
.rule-badge { @apply rounded-md border px-1.5 py-0.5 text-xs font-medium leading-4 transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500; }
.rule-cny { @apply border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-500/10 dark:text-emerald-300; }
.rule-usd { @apply border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-500/10 dark:text-blue-300; }
.rule-tiered { @apply border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-800 dark:bg-violet-500/10 dark:text-violet-300; }
.rule-timed { @apply border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-500/10 dark:text-amber-300; }
.rule-neutral { @apply border-gray-200 bg-gray-50 text-gray-600 dark:border-dark-600 dark:bg-dark-700 dark:text-dark-200; }
.rule-unknown { @apply border-dashed border-gray-300 text-gray-500 dark:border-dark-500 dark:text-dark-300; }
</style>
